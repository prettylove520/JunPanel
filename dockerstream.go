package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var volumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// open sends a request without the overall client timeout, for long transfers
// bounded by ctx. The caller must close the body and check the status.
func (d *dockerClient) open(ctx context.Context, method, apiPath, contentType string, body io.Reader) (*http.Response, error) {
	prefix, e := d.negotiate()
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://docker"+prefix+apiPath, body)
	if e != nil {
		return nil, e
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := *d.HTTP
	client.Timeout = 0
	resp, e := client.Do(req)
	if e != nil {
		return nil, bad(502, "Docker 未响应；操作结果请刷新核对")
	}
	return resp, nil
}
func dockerStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg := fmt.Sprintf("Docker 拒绝操作（HTTP %d），请经 SSH 检查服务", resp.StatusCode)
	if resp.StatusCode == 404 {
		msg = "Docker 中未找到对应的容器、路径或卷"
	}
	return bad(502, msg)
}

// containerPath accepts only clean absolute paths inside a container.
func containerPath(p string) error {
	if len(p) < 1 || len(p) > 512 || !strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.ContainsAny(p, "\x00\\") {
		return bad(400, "容器内路径无效")
	}
	return nil
}

// getArchive streams a tar of a container path; the container may be stopped.
func (d *dockerClient) getArchive(ctx context.Context, id, p string) (io.ReadCloser, error) {
	if !containerID.MatchString(id) {
		return nil, bad(400, "容器 ID 无效")
	}
	if e := containerPath(p); e != nil {
		return nil, e
	}
	resp, e := d.open(ctx, "GET", "/containers/"+id+"/archive?path="+url.QueryEscape(p), "", nil)
	if e != nil {
		return nil, e
	}
	if e = dockerStatus(resp); e != nil {
		resp.Body.Close()
		return nil, e
	}
	return resp.Body, nil
}

// putArchive extracts a tar stream into a directory of a (possibly stopped) container.
func (d *dockerClient) putArchive(ctx context.Context, id, dir string, tarball io.Reader) error {
	if !containerID.MatchString(id) {
		return bad(400, "容器 ID 无效")
	}
	if e := containerPath(dir); e != nil {
		return e
	}
	resp, e := d.open(ctx, "PUT", "/containers/"+id+"/archive?path="+url.QueryEscape(dir), "application/x-tar", tarball)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return dockerStatus(resp)
}

// removeVolume deletes a named volume; a missing volume is not an error.
func (d *dockerClient) removeVolume(ctx context.Context, name string) error {
	if !volumeName.MatchString(name) {
		return bad(400, "卷名称无效")
	}
	resp, e := d.open(ctx, "DELETE", "/volumes/"+url.PathEscape(name), "", nil)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == 404 {
		return nil
	}
	if resp.StatusCode == 409 {
		return bad(409, "卷仍被容器使用，无法删除")
	}
	return dockerStatus(resp)
}

// demux splits a Docker multiplexed stream into stdout and stderr.
func demux(r io.Reader, stdout, stderr io.Writer) error {
	var header [8]byte
	buf := make([]byte, 32<<10)
	for {
		if _, e := io.ReadFull(r, header[:]); e != nil {
			if e == io.EOF {
				return nil
			}
			return bad(502, "数据库输出流不完整")
		}
		if header[0] != 1 && header[0] != 2 || header[1] != 0 || header[2] != 0 || header[3] != 0 {
			return bad(502, "数据库输出流格式无效")
		}
		out := stdout
		if header[0] == 2 {
			out = stderr
		}
		left := int64(binary.BigEndian.Uint32(header[4:8]))
		for left > 0 {
			n := int64(len(buf))
			if left < n {
				n = left
			}
			got, e := io.ReadFull(r, buf[:n])
			if got > 0 {
				if _, we := out.Write(buf[:got]); we != nil {
					return we
				}
			}
			if e != nil {
				return bad(502, "数据库输出流不完整")
			}
			left -= int64(got)
		}
	}
}

// execStream runs a command in a container and streams its stdout to out. The
// returned text is the bounded stderr; a non-zero exit status is an error.
func (d *dockerClient) execStream(ctx context.Context, id string, args, env []string, out io.Writer) (string, error) {
	if !containerID.MatchString(id) {
		return "", bad(400, "容器 ID 无效")
	}
	raw, e := d.call("POST", "/containers/"+id+"/exec", map[string]any{"AttachStdout": true, "AttachStderr": true, "Tty": false, "Cmd": args, "Env": env})
	if e != nil {
		return "", e
	}
	var created struct{ Id string }
	if e = decodeObject(raw, &created); e != nil {
		return "", e
	}
	if !containerID.MatchString(created.Id) {
		return "", bad(502, "Docker exec 标识无效")
	}
	payload := strings.NewReader(`{"Detach":false,"Tty":false}`)
	resp, e := d.open(ctx, "POST", "/exec/"+created.Id+"/start", "application/json", payload)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if e = dockerStatus(resp); e != nil {
		return "", e
	}
	stderr := &limitedBuffer{Max: 64 << 10}
	if e = demux(resp.Body, out, stderr); e != nil {
		return cleanText(stderr.String()), e
	}
	raw, e = d.call("GET", "/exec/"+created.Id+"/json", nil)
	if e != nil {
		return "", e
	}
	var status struct {
		ExitCode int
		Running  bool
	}
	if e = decodeObject(raw, &status); e != nil {
		return "", e
	}
	text := cleanText(stderr.String())
	if status.Running {
		return text, bad(409, "数据库命令仍在执行，请通过 SSH 检查")
	}
	if status.ExitCode != 0 {
		return text, bad(400, "数据库命令失败："+truncate(text, 3000))
	}
	return text, nil
}

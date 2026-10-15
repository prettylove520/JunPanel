package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

type job struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	State   string     `json:"state"`
	Started time.Time  `json:"started"`
	Ended   *time.Time `json:"ended,omitempty"`
	Output  string     `json:"output"`
	Error   string     `json:"error,omitempty"`
	buffer  *limitedBuffer
}
type jobQueue struct {
	sync.Mutex
	items map[string]*job
	slots chan struct{}
}

func newQueue() *jobQueue { return &jobQueue{items: map[string]*job{}, slots: make(chan struct{}, 2)} }
func (q *jobQueue) start(title string, fn func(context.Context, *limitedBuffer) error) (job, error) {
	select {
	case q.slots <- struct{}{}:
	default:
		return job{}, bad(409, "已有两个后台任务在运行，请稍后重试")
	}
	q.Lock()
	if len(q.items) >= 80 {
		var oldest string
		var at time.Time
		for id, j := range q.items {
			if j.State != "running" && (oldest == "" || j.Started.Before(at)) {
				oldest = id
				at = j.Started
			}
		}
		if oldest != "" {
			delete(q.items, oldest)
		}
	}
	j := &job{ID: randomID(24), Title: title, State: "running", Started: time.Now(), buffer: &limitedBuffer{Max: 128 << 10}}
	q.items[j.ID] = j
	copy := *j
	q.Unlock()
	go func() {
		defer func() { <-q.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		out := j.buffer
		var e error
		func() {
			defer func() {
				if recover() != nil {
					e = fmt.Errorf("任务发生内部错误")
				}
			}()
			e = fn(ctx, out)
		}()
		q.Lock()
		defer q.Unlock()
		now := time.Now()
		j.Ended = &now
		j.Output = cleanText(out.String())
		j.State = "success"
		if e != nil {
			j.State = "failed"
			j.Error = e.Error()
		}
		log.Printf("job id=%s result=%s", j.ID, j.State)
	}()
	return copy, nil
}
func (q *jobQueue) list() []job {
	q.Lock()
	defer q.Unlock()
	out := make([]job, 0, len(q.items))
	for _, j := range q.items {
		copy := *j
		if j.buffer != nil {
			copy.Output = cleanText(j.buffer.String())
		}
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

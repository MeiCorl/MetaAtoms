package background

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	subagentruntime "github.com/metaatoms/metaatoms/src/subagent/runtime"
)

const (
	EventQueued       EventType = "queued"
	EventRunning      EventType = "running"
	EventBackgrounded EventType = "backgrounded"
	EventCompleted    EventType = "completed"
	EventFailed       EventType = "failed"
	EventCanceled     EventType = "canceled"
)

var (
	ErrTaskNotFound      = errors.New("subagent background: task not found")
	ErrTaskAlreadyDone   = errors.New("subagent background: task already finished")
	ErrTaskRunnerMissing = errors.New("subagent background: task runner is nil")
)

// RunFunc runs one child agent invocation. It is intentionally narrow so Task 6
// can wrap defined and fork runner calls without coupling this package to tool schema.
type RunFunc func(context.Context) (*subagentruntime.RunResult, error)

// EventType identifies why a task notification was emitted.
type EventType string

// Event is emitted every time a task is created, starts, enters background, or ends.
type Event struct {
	Type EventType    `json:"type"`
	Task TaskSnapshot `json:"task"`
}

// NotifyFunc receives task lifecycle updates. Implementations should return quickly.
type NotifyFunc func(Event)

// ManagerOptions configures the process-local task manager.
type ManagerOptions struct {
	DefaultForegroundTimeout time.Duration
	Notify                   NotifyFunc
	Now                      func() time.Time
}

// SubmitRequest describes one task submission to the background manager.
type SubmitRequest struct {
	Type              string
	RoleName          string
	Prompt            subagentruntime.PromptTrace
	Background        bool
	ForegroundTimeout time.Duration
	Run               RunFunc
}

// SubmitResult is returned to the caller that started a task.
type SubmitResult struct {
	Task         TaskSnapshot               `json:"task"`
	Result       *subagentruntime.RunResult `json:"result,omitempty"`
	Backgrounded bool                       `json:"backgrounded"`
	Reason       string                     `json:"reason,omitempty"`
}

type taskRecord struct {
	snapshot     TaskSnapshot
	cancel       context.CancelFunc
	done         chan struct{}
	backgrounded chan struct{}
	result       *subagentruntime.RunResult
	err          error
}

// Manager tracks in-process SubAgent tasks and emits lifecycle notifications.
type Manager struct {
	mu        sync.RWMutex
	tasks     map[string]*taskRecord
	nextID    atomic.Uint64
	defaultTO time.Duration
	notify    NotifyFunc
	now       func() time.Time
}

func NewManager(opts ManagerOptions) *Manager {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		tasks:     make(map[string]*taskRecord),
		defaultTO: opts.DefaultForegroundTimeout,
		notify:    opts.Notify,
		now:       now,
	}
}

// Submit creates and starts a task. Background submissions return immediately;
// foreground submissions wait until completion or until their foreground timeout
// elapses, at which point the task is marked backgrounded and keeps running.
func (m *Manager) Submit(ctx context.Context, req SubmitRequest) (SubmitResult, error) {
	if m == nil {
		return SubmitResult{}, errors.New("subagent background: manager is nil")
	}
	if req.Run == nil {
		return SubmitResult{}, ErrTaskRunnerMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}

	taskCtx, cancel := context.WithCancel(context.Background())
	rec := &taskRecord{
		snapshot: TaskSnapshot{
			ID:         m.nextTaskID(),
			Type:       req.Type,
			RoleName:   req.RoleName,
			Status:     StatusQueued,
			CreatedAt:  m.now(),
			Background: req.Background,
			Prompt:     req.Prompt,
		},
		cancel:       cancel,
		done:         make(chan struct{}),
		backgrounded: make(chan struct{}),
	}
	if req.Background {
		rec.snapshot.BackgroundReason = BackgroundReasonExplicit
	}

	m.mu.Lock()
	m.tasks[rec.snapshot.ID] = rec
	queued := cloneSnapshot(rec.snapshot)
	m.mu.Unlock()
	m.emit(EventQueued, queued)

	go m.run(taskCtx, rec, req.Run)

	if req.Background {
		return SubmitResult{Task: queued, Backgrounded: true, Reason: BackgroundReasonExplicit}, nil
	}

	timeout := req.ForegroundTimeout
	if timeout <= 0 {
		timeout = m.defaultTO
	}
	if timeout <= 0 {
		select {
		case <-rec.done:
			return m.completedSubmitResult(rec)
		case <-rec.backgrounded:
			return m.backgroundedSubmitResult(rec)
		case <-ctx.Done():
			snap, _ := m.moveToBackground(rec.snapshot.ID, BackgroundReasonTimeout)
			return SubmitResult{Task: snap, Backgrounded: true, Reason: BackgroundReasonTimeout}, nil
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-rec.done:
		return m.completedSubmitResult(rec)
	case <-rec.backgrounded:
		return m.backgroundedSubmitResult(rec)
	case <-timer.C:
		snap, err := m.moveToBackground(rec.snapshot.ID, BackgroundReasonTimeout)
		if err != nil {
			return m.completedSubmitResult(rec)
		}
		return SubmitResult{Task: snap, Backgrounded: true, Reason: BackgroundReasonTimeout}, nil
	case <-ctx.Done():
		snap, _ := m.moveToBackground(rec.snapshot.ID, BackgroundReasonTimeout)
		return SubmitResult{Task: snap, Backgrounded: true, Reason: BackgroundReasonTimeout}, nil
	}
}

func (m *Manager) run(ctx context.Context, rec *taskRecord, run RunFunc) {
	m.markRunning(rec.snapshot.ID)
	result, err := run(ctx)
	m.finish(rec.snapshot.ID, result, err)
}

func (m *Manager) markRunning(id string) {
	var snap TaskSnapshot
	ok := false
	m.mu.Lock()
	if rec := m.tasks[id]; rec != nil && rec.snapshot.Status == StatusQueued {
		rec.snapshot.Status = StatusRunning
		rec.snapshot.StartedAt = m.now()
		snap = cloneSnapshot(rec.snapshot)
		ok = true
	}
	m.mu.Unlock()
	if ok {
		m.emit(EventRunning, snap)
	}
}

func (m *Manager) finish(id string, result *subagentruntime.RunResult, err error) {
	var snap TaskSnapshot
	var typ EventType
	m.mu.Lock()
	rec := m.tasks[id]
	if rec == nil {
		m.mu.Unlock()
		return
	}
	rec.result = result
	rec.err = err
	if rec.snapshot.EndedAt.IsZero() {
		rec.snapshot.EndedAt = m.now()
	}
	if result != nil {
		applyRunResult(&rec.snapshot, result)
	}
	if rec.snapshot.Status == StatusCanceled {
		typ = EventCanceled
	} else if isCanceledResult(result, err) {
		rec.snapshot.Status = StatusCanceled
		rec.snapshot.Error = firstErrorText(err, result)
		typ = EventCanceled
	} else if isFailedResult(result, err) {
		rec.snapshot.Status = StatusFailed
		rec.snapshot.Error = firstErrorText(err, result)
		typ = EventFailed
	} else {
		rec.snapshot.Status = StatusCompleted
		typ = EventCompleted
	}
	snap = cloneSnapshot(rec.snapshot)
	close(rec.done)
	m.mu.Unlock()
	m.emit(typ, snap)
}

func applyRunResult(task *TaskSnapshot, result *subagentruntime.RunResult) {
	task.Type = result.Type
	task.RoleName = result.RoleName
	task.FinalText = result.FinalText
	task.StructuredOutput = cloneMap(result.StructuredOutput)
	task.Iterations = result.Iterations
	task.ToolCalls = result.ToolCalls
	task.Usage = result.Usage
	task.Error = result.Error
	trace := result.Trace
	trace.Prompt.SystemBlocks = nil
	task.Trace = &trace
	task.Output = result.Trace.Output
	if !result.Trace.StartedAt.IsZero() {
		task.StartedAt = result.Trace.StartedAt
	}
	if !result.Trace.EndedAt.IsZero() {
		task.EndedAt = result.Trace.EndedAt
	}
	if task.Prompt.Type == "" {
		task.Prompt = result.Trace.Prompt
	}
	task.Prompt.SystemBlocks = nil
}

func isCanceledResult(result *subagentruntime.RunResult, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return result != nil && result.Status == subagentruntime.RunStatusAborted
}

func isFailedResult(result *subagentruntime.RunResult, err error) bool {
	if err != nil {
		return true
	}
	return result != nil && result.Status == subagentruntime.RunStatusFailed
}

func firstErrorText(err error, result *subagentruntime.RunResult) string {
	if result != nil && result.Error != "" {
		return result.Error
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func (m *Manager) completedSubmitResult(rec *taskRecord) (SubmitResult, error) {
	<-rec.done
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := cloneSnapshot(rec.snapshot)
	return SubmitResult{Task: snap, Result: rec.result}, rec.err
}

func (m *Manager) backgroundedSubmitResult(rec *taskRecord) (SubmitResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := cloneSnapshot(rec.snapshot)
	return SubmitResult{Task: snap, Backgrounded: true, Reason: snap.BackgroundReason}, nil
}

func (m *Manager) nextTaskID() string {
	seq := m.nextID.Add(1)
	return fmt.Sprintf("subagent-task-%d", seq)
}

// MoveToBackground marks a queued or running foreground task as backgrounded.
func (m *Manager) MoveToBackground(id string) (TaskSnapshot, error) {
	return m.moveToBackground(id, BackgroundReasonManual)
}

func (m *Manager) moveToBackground(id, reason string) (TaskSnapshot, error) {
	var snap TaskSnapshot
	m.mu.Lock()
	rec := m.tasks[id]
	if rec == nil {
		m.mu.Unlock()
		return TaskSnapshot{}, ErrTaskNotFound
	}
	if isTerminal(rec.snapshot.Status) {
		snap = cloneSnapshot(rec.snapshot)
		m.mu.Unlock()
		return snap, ErrTaskAlreadyDone
	}
	if rec.snapshot.Background {
		snap = cloneSnapshot(rec.snapshot)
		m.mu.Unlock()
		return snap, nil
	}
	rec.snapshot.Background = true
	rec.snapshot.BackgroundReason = reason
	close(rec.backgrounded)
	snap = cloneSnapshot(rec.snapshot)
	m.mu.Unlock()
	m.emit(EventBackgrounded, snap)
	return snap, nil
}

// Cancel requests cancellation for a queued or running task.
func (m *Manager) Cancel(id string) (TaskSnapshot, error) {
	var snap TaskSnapshot
	var cancel context.CancelFunc
	m.mu.Lock()
	rec := m.tasks[id]
	if rec == nil {
		m.mu.Unlock()
		return TaskSnapshot{}, ErrTaskNotFound
	}
	if isTerminal(rec.snapshot.Status) {
		snap = cloneSnapshot(rec.snapshot)
		m.mu.Unlock()
		return snap, ErrTaskAlreadyDone
	}
	rec.snapshot.Status = StatusCanceled
	rec.snapshot.EndedAt = m.now()
	rec.snapshot.Error = context.Canceled.Error()
	cancel = rec.cancel
	snap = cloneSnapshot(rec.snapshot)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.emit(EventCanceled, snap)
	return snap, nil
}

// CancelActive requests cancellation for every queued or running task and
// returns the snapshots that were marked canceled immediately.
func (m *Manager) CancelActive() []TaskSnapshot {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	ids := make([]string, 0, len(m.tasks))
	for id, rec := range m.tasks {
		if rec == nil || isTerminal(rec.snapshot.Status) {
			continue
		}
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	sort.Strings(ids)

	out := make([]TaskSnapshot, 0, len(ids))
	for _, id := range ids {
		snap, err := m.Cancel(id)
		if err == nil {
			out = append(out, snap)
		}
	}
	return out
}

func (m *Manager) Get(id string) (TaskSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec := m.tasks[id]
	if rec == nil {
		return TaskSnapshot{}, false
	}
	return cloneSnapshot(rec.snapshot), true
}

func (m *Manager) List() []TaskSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TaskSnapshot, 0, len(m.tasks))
	for _, rec := range m.tasks {
		out = append(out, cloneSnapshot(rec.snapshot))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (m *Manager) emit(eventType EventType, snap TaskSnapshot) {
	if m.notify == nil {
		return
	}
	m.notify(Event{Type: eventType, Task: snap})
}

func isTerminal(status TaskStatus) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

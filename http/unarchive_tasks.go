package fbhttp

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
)

type UnarchiveTaskStatus string

const (
	UnarchiveTaskRunning UnarchiveTaskStatus = "running"
	UnarchiveTaskSuccess UnarchiveTaskStatus = "success"
	UnarchiveTaskFailed  UnarchiveTaskStatus = "failed"
)

type UnarchiveTask struct {
	ID          uint64              `json:"id"`
	Source      string              `json:"source"`
	Destination string              `json:"destination"`
	ArchiveName string              `json:"archiveName"`
	Status      UnarchiveTaskStatus `json:"status"`
	Error       string              `json:"error,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	FinishedAt  *time.Time          `json:"finishedAt,omitempty"`

	userID uint
}

type UnarchiveTaskManager struct {
	mu           sync.RWMutex
	nextID       atomic.Uint64
	limitPerUser int
	byUser       map[uint][]*UnarchiveTask
	byID         map[uint64]*UnarchiveTask
}

func NewUnarchiveTaskManager(limitPerUser int) *UnarchiveTaskManager {
	if limitPerUser <= 0 {
		limitPerUser = 100
	}

	return &UnarchiveTaskManager{
		limitPerUser: limitPerUser,
		byUser:       map[uint][]*UnarchiveTask{},
		byID:         map[uint64]*UnarchiveTask{},
	}
}

func (m *UnarchiveTaskManager) Create(userID uint, source, destination, archiveName string) UnarchiveTask {
	task := &UnarchiveTask{
		ID:          m.nextID.Add(1),
		Source:      source,
		Destination: destination,
		ArchiveName: archiveName,
		Status:      UnarchiveTaskRunning,
		CreatedAt:   time.Now().UTC(),
		userID:      userID,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.byID[task.ID] = task
	m.byUser[userID] = append([]*UnarchiveTask{task}, m.byUser[userID]...)
	if len(m.byUser[userID]) > m.limitPerUser {
		removed := m.byUser[userID][m.limitPerUser:]
		m.byUser[userID] = m.byUser[userID][:m.limitPerUser]
		for _, item := range removed {
			delete(m.byID, item.ID)
		}
	}

	return task.clone()
}

func (m *UnarchiveTaskManager) Success(taskID uint64) {
	finishedAt := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.byID[taskID]
	if !ok {
		return
	}

	task.Status = UnarchiveTaskSuccess
	task.Error = ""
	task.FinishedAt = &finishedAt
}

func (m *UnarchiveTaskManager) Fail(taskID uint64, err error) {
	finishedAt := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.byID[taskID]
	if !ok {
		return
	}

	task.Status = UnarchiveTaskFailed
	task.Error = err.Error()
	task.FinishedAt = &finishedAt
}

func (m *UnarchiveTaskManager) List(userID uint) []UnarchiveTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]UnarchiveTask, 0, len(m.byUser[userID]))
	for _, task := range m.byUser[userID] {
		tasks = append(tasks, task.clone())
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].ID > tasks[j].ID
	})

	return tasks
}

func (m *UnarchiveTaskManager) Delete(userID uint, taskID uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.byID[taskID]
	if !ok || task.userID != userID {
		return false
	}

	delete(m.byID, taskID)
	tasks := m.byUser[userID]
	for i, item := range tasks {
		if item.ID == taskID {
			m.byUser[userID] = append(tasks[:i], tasks[i+1:]...)
			break
		}
	}

	return true
}

func (t *UnarchiveTask) clone() UnarchiveTask {
	cloned := *t
	return cloned
}

var unarchiveTasksGetHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	return renderJSON(w, r, d.unarchiveTasks.List(d.user.ID))
})

var unarchiveTaskDeleteHandler = withUser(func(_ http.ResponseWriter, r *http.Request, d *data) (int, error) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return http.StatusBadRequest, err
	}

	if !d.unarchiveTasks.Delete(d.user.ID, id) {
		return http.StatusNotFound, nil
	}

	return http.StatusNoContent, nil
})

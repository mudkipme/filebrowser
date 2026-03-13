package fbhttp

import (
	"errors"
	"testing"
)

func TestUnarchiveTaskManagerLifecycle(t *testing.T) {
	t.Parallel()

	manager := NewUnarchiveTaskManager(10)
	task := manager.Create(42, "/archive.zip", "/archive", "archive.zip")

	tasks := manager.List(42)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != UnarchiveTaskRunning {
		t.Fatalf("expected running status, got %q", tasks[0].Status)
	}

	manager.Fail(task.ID, errors.New("boom"))
	tasks = manager.List(42)
	if tasks[0].Status != UnarchiveTaskFailed {
		t.Fatalf("expected failed status, got %q", tasks[0].Status)
	}
	if tasks[0].Error != "boom" {
		t.Fatalf("expected persisted error, got %q", tasks[0].Error)
	}

	manager.Success(task.ID)
	tasks = manager.List(42)
	if tasks[0].Status != UnarchiveTaskSuccess {
		t.Fatalf("expected success status, got %q", tasks[0].Status)
	}

	if !manager.Delete(42, task.ID) {
		t.Fatal("expected task delete to succeed")
	}
	if len(manager.List(42)) != 0 {
		t.Fatal("expected task list to be empty after delete")
	}
}

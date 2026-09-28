package display

import (
	"encoding/json"
	"strings"
)

const (
	maxTodoArgsBytes = 64 << 10
	maxTodoItems     = 100
	maxTodoTextBytes = 512
)

// Todo is one complete todo_write revision, ordered exactly as the agent wrote it.
type Todo struct {
	Items []TodoItem `json:"items"`
}

// TodoItem keeps the tool's semantic status and optional active wording.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
}

// decodeTodoPreview promotes arguments only when the actual successful tool
// result agrees with them line for line. An error or truncated result stays raw.
func decodeTodoPreview(in PreviewInput) (Todo, bool) {
	if len(in.Arguments) > maxTodoArgsBytes || len(in.ResultPreview) > maxTodoArgsBytes {
		return Todo{}, false
	}
	var args struct {
		Todos *[]struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(in.Arguments), &args); err != nil || args.Todos == nil || len(*args.Todos) > maxTodoItems {
		return Todo{}, false
	}
	items := make([]TodoItem, 0, len(*args.Todos))
	lines := make([]string, 0, len(*args.Todos))
	active := 0
	for _, item := range *args.Todos {
		if strings.TrimSpace(item.Content) == "" || len(item.Content) > maxTodoTextBytes || len(item.ActiveForm) > maxTodoTextBytes || strings.ContainsAny(item.Content, "\r\n") || strings.ContainsAny(item.ActiveForm, "\r\n") {
			return Todo{}, false
		}
		mark := ""
		switch item.Status {
		case "pending":
			mark = "[ ]"
		case "in_progress":
			mark = "[~]"
			active++
		case "completed":
			mark = "[x]"
		default:
			return Todo{}, false
		}
		if active > 1 {
			return Todo{}, false
		}
		lines = append(lines, mark+" "+item.Content)
		items = append(items, TodoItem{Content: item.Content, Status: item.Status, ActiveForm: item.ActiveForm})
	}
	want := strings.Join(lines, "\n")
	if len(items) == 0 {
		want = "[todo list cleared]"
	}
	if in.ResultPreview != want {
		return Todo{}, false
	}
	return Todo{Items: items}, true
}

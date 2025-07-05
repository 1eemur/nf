package main

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nsf/termbox-go"
)

// Task represents a task or subtask
type Task struct {
	ID         int
	Title      string
	Priority   int
	CreatedAt  time.Time
	ParentID   *int
	Children   []*Task
	IsExpanded bool
}

// TaskManager handles all task operations
type TaskManager struct {
	db            *sql.DB
	tasks         []*Task
	currentIndex  int
	scrollOffset  int
	flatView      []*Task
	editMode      bool
	editBuffer    string
	statusMsg     string
	inputMode     string // "add", "addsubtask", "edit", ""
	inputStep     int    // 0 = title, 1 = priority
	inputTitle    string
	inputPriority string
}

// NewTaskManager creates a new task manager instance
func NewTaskManager() *TaskManager {
	tm := &TaskManager{
		tasks:        make([]*Task, 0),
		currentIndex: 0,
		scrollOffset: 0,
		flatView:     make([]*Task, 0),
	}

	if err := tm.initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	tm.loadTasks()
	tm.rebuildFlatView()

	return tm
}

// initDB initializes the SQLite database
func (tm *TaskManager) initDB() error {
	var err error
	tm.db, err = sql.Open("sqlite3", "tasks.db")
	if err != nil {
		return err
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		priority INTEGER DEFAULT 50,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		parent_id INTEGER,
		FOREIGN KEY (parent_id) REFERENCES tasks (id) ON DELETE CASCADE
	);`

	_, err = tm.db.Exec(createTableSQL)
	return err
}

// loadTasks loads all tasks from database
func (tm *TaskManager) loadTasks() error {
	rows, err := tm.db.Query(`
		SELECT id, title, priority, created_at, parent_id 
		FROM tasks 
		ORDER BY priority DESC, created_at ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	taskMap := make(map[int]*Task)
	tm.tasks = make([]*Task, 0)

	for rows.Next() {
		task := &Task{Children: make([]*Task, 0), IsExpanded: true}
		var parentID sql.NullInt64
		var createdAt string

		err := rows.Scan(&task.ID, &task.Title, &task.Priority, &createdAt, &parentID)
		if err != nil {
			return err
		}

		task.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)

		if parentID.Valid {
			task.ParentID = new(int)
			*task.ParentID = int(parentID.Int64)
		}

		taskMap[task.ID] = task
	}

	// Build hierarchy
	for _, task := range taskMap {
		if task.ParentID == nil {
			tm.tasks = append(tm.tasks, task)
		} else {
			parent := taskMap[*task.ParentID]
			if parent != nil {
				parent.Children = append(parent.Children, task)
			}
		}
	}

	// Sort root tasks and children by priority
	tm.sortTasks(tm.tasks)
	for _, task := range taskMap {
		tm.sortTasks(task.Children)
	}

	return nil
}

// sortTasks sorts tasks by priority (high to low) then creation time
func (tm *TaskManager) sortTasks(tasks []*Task) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority // Higher priority first
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
}

// rebuildFlatView creates a flat view of tasks for navigation
func (tm *TaskManager) rebuildFlatView() {
	tm.flatView = make([]*Task, 0)
	tm.buildFlatView(tm.tasks, 0)

	if tm.currentIndex >= len(tm.flatView) {
		tm.currentIndex = len(tm.flatView) - 1
	}
	if tm.currentIndex < 0 {
		tm.currentIndex = 0
	}

	tm.adjustScroll()
}

// adjustScroll adjusts the scroll offset to keep the current item visible
func (tm *TaskManager) adjustScroll() {
	_, height := termbox.Size()
	maxVisibleTasks := height - 4 // Account for header, status, and input lines

	if maxVisibleTasks <= 0 {
		return
	}

	// Ensure current item is visible
	if tm.currentIndex < tm.scrollOffset {
		tm.scrollOffset = tm.currentIndex
	} else if tm.currentIndex >= tm.scrollOffset+maxVisibleTasks {
		tm.scrollOffset = tm.currentIndex - maxVisibleTasks + 1
	}

	// Ensure we don't scroll past the end
	if tm.scrollOffset > len(tm.flatView)-maxVisibleTasks {
		tm.scrollOffset = len(tm.flatView) - maxVisibleTasks
	}

	// Ensure we don't scroll past the beginning
	if tm.scrollOffset < 0 {
		tm.scrollOffset = 0
	}
}

// buildFlatView recursively builds the flat view
func (tm *TaskManager) buildFlatView(tasks []*Task, depth int) {
	for _, task := range tasks {
		tm.flatView = append(tm.flatView, task)
		if task.IsExpanded && len(task.Children) > 0 {
			tm.buildFlatView(task.Children, depth+1)
		}
	}
}

// addTask adds a new task
func (tm *TaskManager) addTask(title string, priority int, parentID *int) error {
	var result sql.Result
	var err error

	if parentID != nil {
		result, err = tm.db.Exec(
			"INSERT INTO tasks (title, priority, parent_id) VALUES (?, ?, ?)",
			title, priority, *parentID,
		)
	} else {
		result, err = tm.db.Exec(
			"INSERT INTO tasks (title, priority) VALUES (?, ?)",
			title, priority,
		)
	}

	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	tm.statusMsg = fmt.Sprintf("Added task: %s (ID: %d)", title, id)

	tm.loadTasks()
	tm.rebuildFlatView()
	return nil
}

// deleteTask deletes a task and its children
func (tm *TaskManager) deleteTask(taskID int) error {
	_, err := tm.db.Exec("DELETE FROM tasks WHERE id = ? OR parent_id = ?", taskID, taskID)
	if err != nil {
		return err
	}

	tm.statusMsg = fmt.Sprintf("Deleted task ID: %d", taskID)
	tm.loadTasks()
	tm.rebuildFlatView()
	return nil
}

// updateTask updates a task's title and priority
func (tm *TaskManager) updateTask(taskID int, title string, priority int) error {
	_, err := tm.db.Exec(
		"UPDATE tasks SET title = ?, priority = ? WHERE id = ?",
		title, priority, taskID,
	)
	if err != nil {
		return err
	}

	tm.statusMsg = fmt.Sprintf("Updated task ID: %d", taskID)
	tm.loadTasks()
	tm.rebuildFlatView()
	return nil
}

// updateTaskPriority updates only a task's priority
func (tm *TaskManager) updateTaskPriority(taskID int, priorityChange int) error {
	// First get current priority
	var currentPriority int
	err := tm.db.QueryRow("SELECT priority FROM tasks WHERE id = ?", taskID).Scan(&currentPriority)
	if err != nil {
		return err
	}

	newPriority := currentPriority + priorityChange

	// Clamp priority between 1 and 100
	if newPriority < 1 {
		newPriority = 1
	}
	if newPriority > 100 {
		newPriority = 100
	}

	_, err = tm.db.Exec("UPDATE tasks SET priority = ? WHERE id = ?", newPriority, taskID)
	if err != nil {
		return err
	}

	tm.statusMsg = fmt.Sprintf("Updated task priority: %d -> %d", currentPriority, newPriority)
	tm.loadTasks()
	tm.rebuildFlatView()
	return nil
}

// formatDuration formats time duration for display
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	} else if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	} else {
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// getTaskDepth returns the depth of a task in the hierarchy
func (tm *TaskManager) getTaskDepth(task *Task) int {
	for _, t := range tm.flatView {
		if t.ID == task.ID {
			// Count preceding tasks with same or higher depth
			depth := 0
			if task.ParentID != nil {
				for _, parent := range tm.flatView {
					if parent.ID == *task.ParentID {
						depth = tm.getTaskDepth(parent) + 1
						break
					}
				}
			}
			return depth
		}
	}
	return 0
}

// render renders the task list
func (tm *TaskManager) render() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

	width, height := termbox.Size()

	// Header with better styling
	header := "Task Manager - j/k: navigate, Shift+j: priority -1, Shift+k: priority +1, a: add, s: subtask, d: delete, e: edit, space: toggle, q: quit"
	for i, r := range header {
		if i >= width {
			break
		}
		termbox.SetCell(i, 0, r, termbox.ColorWhite|termbox.AttrBold, termbox.ColorBlue)
	}

	// Status message with better contrast
	if tm.statusMsg != "" {
		for i, r := range tm.statusMsg {
			if i >= width {
				break
			}
			termbox.SetCell(i, 1, r, termbox.ColorBlack, termbox.ColorGreen)
		}
	}

	// Tasks
	startY := 3
	_, height = termbox.Size()
	maxVisibleTasks := height - startY - 1

	startIdx := tm.scrollOffset
	endIdx := tm.scrollOffset + maxVisibleTasks
	if endIdx > len(tm.flatView) {
		endIdx = len(tm.flatView)
	}

	for i := startIdx; i < endIdx; i++ {
		task := tm.flatView[i]
		y := startY + (i - startIdx)

		// Improved highlighting with better contrast
		bg := termbox.ColorDefault
		fg := termbox.ColorDefault
		if i == tm.currentIndex {
			bg = termbox.ColorWhite
			fg = termbox.ColorBlack | termbox.AttrBold
		}

		depth := tm.getTaskDepth(task)
		indent := strings.Repeat("  ", depth)

		// Better visual indicators
		prefix := ""
		if task.ParentID != nil {
			prefix = "├─ "
		}

		expansion := ""
		if len(task.Children) > 0 {
			if task.IsExpanded {
				expansion = "▼ "
			} else {
				expansion = "▶ "
			}
		}

		timeAgo := formatDuration(time.Since(task.CreatedAt))

		// Priority color coding for better visual hierarchy
		priorityColor := termbox.ColorDefault
		if task.Priority >= 80 {
			priorityColor = termbox.ColorRed | termbox.AttrBold
		} else if task.Priority >= 60 {
			priorityColor = termbox.ColorYellow
		} else if task.Priority <= 20 {
			priorityColor = termbox.ColorBlue
		}

		// If this is the selected row, override priority color
		if i == tm.currentIndex {
			priorityColor = fg
		}

		// Clear the entire line first
		for j := 0; j < width; j++ {
			termbox.SetCell(j, y, ' ', fg, bg)
		}

		// Render the task line
		line := fmt.Sprintf("%s%s%s%s", indent, expansion, prefix, task.Title)
		for j, r := range line {
			if j >= width-25 { // Leave space for priority and time
				break
			}
			termbox.SetCell(j, y, r, fg, bg)
		}

		// Render priority and time info on the right side
		rightInfo := fmt.Sprintf("P:%d %s", task.Priority, timeAgo)
		rightStart := width - len(rightInfo)
		if rightStart > 0 {
			for j, r := range rightInfo {
				if j < 4 { // Priority part
					termbox.SetCell(rightStart+j, y, r, priorityColor, bg)
				} else { // Time part
					termbox.SetCell(rightStart+j, y, r, termbox.ColorCyan, bg)
				}
			}
		}
	}

	// Scroll indicator with better styling
	if len(tm.flatView) > maxVisibleTasks {
		scrollY := height - 1
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d tasks", startIdx+1, endIdx, len(tm.flatView))
		for i, r := range scrollInfo {
			if i >= width {
				break
			}
			termbox.SetCell(i, scrollY, r, termbox.ColorWhite, termbox.ColorBlue)
		}
	}

	// Edit mode with better styling
	if tm.editMode {
		editY := height - 2
		editPrompt := "Edit (title:priority): " + tm.editBuffer
		for i, r := range editPrompt {
			if i >= width {
				break
			}
			termbox.SetCell(i, editY, r, termbox.ColorBlack, termbox.ColorYellow)
		}
	} else if tm.inputMode != "" {
		inputY := height - 3
		var prompt string

		switch tm.inputMode {
		case "add":
			if tm.inputStep == 0 {
				prompt = "Add Task - Title: " + tm.inputTitle
			} else {
				prompt = fmt.Sprintf("Add Task - Title: %s, Priority (1-100, default 50): %s", tm.inputTitle, tm.inputPriority)
			}
		case "addsubtask":
			if tm.inputStep == 0 {
				prompt = "Add Subtask - Title: " + tm.inputTitle
			} else {
				prompt = fmt.Sprintf("Add Subtask - Title: %s, Priority (1-100, default 50): %s", tm.inputTitle, tm.inputPriority)
			}
		}

		// Clear the input line
		for j := 0; j < width; j++ {
			termbox.SetCell(j, inputY, ' ', termbox.ColorDefault, termbox.ColorDefault)
		}
		for i, r := range prompt {
			if i >= width {
				break
			}
			termbox.SetCell(i, inputY, r, termbox.ColorBlack, termbox.ColorYellow)
		}

		// Help text with better contrast
		helpY := height - 2
		helpText := "Press Enter to confirm, Esc to cancel"
		for j := 0; j < width; j++ {
			termbox.SetCell(j, helpY, ' ', termbox.ColorDefault, termbox.ColorDefault)
		}
		for i, r := range helpText {
			if i >= width {
				break
			}
			termbox.SetCell(i, helpY, r, termbox.ColorWhite, termbox.ColorBlue)
		}
	}

	termbox.Flush()
}

// handleInput handles keyboard input and returns true if should quit
func (tm *TaskManager) handleInput() bool {
	switch ev := termbox.PollEvent(); ev.Type {
	case termbox.EventKey:
		if tm.editMode {
			return tm.handleEditMode(ev)
		} else if tm.inputMode != "" {
			return tm.handleInputMode(ev)
		} else {
			return tm.handleNormalMode(ev)
		}
	}
	return false
}

func (tm *TaskManager) scrollHalfPageDown() {
	_, height := termbox.Size()
	halfPage := (height - 4) / 2 // 4 lines reserved for header/status/input

	tm.currentIndex += halfPage
	if tm.currentIndex >= len(tm.flatView) {
		tm.currentIndex = len(tm.flatView) - 1
	}
	tm.adjustScroll()
}
func (tm *TaskManager) scrollHalfPageUp() {
	_, height := termbox.Size()
	halfPage := (height - 4) / 2

	tm.currentIndex -= halfPage
	if tm.currentIndex < 0 {
		tm.currentIndex = 0
	}
	tm.adjustScroll()
}

// handleNormalMode handles input in normal mode and returns true if should quit
func (tm *TaskManager) handleNormalMode(ev termbox.Event) bool {
	tm.statusMsg = ""

	switch ev.Key {
	case termbox.KeyCtrlC:
		return true
	case termbox.KeyCtrlD:
		tm.scrollHalfPageDown()
	case termbox.KeyCtrlU:
		tm.scrollHalfPageUp()
	case termbox.KeySpace:
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			if len(task.Children) > 0 {
				task.IsExpanded = !task.IsExpanded
				tm.rebuildFlatView()
			}
		}
	}

	switch ev.Ch {
	case 'q':
		return true
	case 'j':
		// Regular j: move down
		if tm.currentIndex < len(tm.flatView)-1 {
			tm.currentIndex++
			tm.adjustScroll()
		}
	case 'k':
		// Regular k: move up
		if tm.currentIndex > 0 {
			tm.currentIndex--
			tm.adjustScroll()
		}
	case 'J':
		// Capital J (Shift+j): decrease priority by 1
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			tm.updateTaskPriority(task.ID, -1)
		}
	case 'K':
		// Capital K (Shift+k): increase priority by 1
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			tm.updateTaskPriority(task.ID, +1)
		}
	case 'g':
		tm.currentIndex = 0
		tm.adjustScroll()
	case 'G':
		tm.currentIndex = len(tm.flatView) - 1
		tm.adjustScroll()
	case 'a':
		tm.inputMode = "add"
		tm.inputStep = 0
		tm.inputTitle = ""
		tm.inputPriority = ""
	case 's':
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			tm.inputMode = "addsubtask"
			tm.inputStep = 0
			tm.inputTitle = ""
			tm.inputPriority = ""
		}
	case 'd':
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			tm.deleteTask(task.ID)
		}
	case 'e':
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			tm.editMode = true
			tm.editBuffer = fmt.Sprintf("%s:%d", task.Title, task.Priority)
		}
	}
	return false
}

// handleInputMode handles input mode for adding tasks
func (tm *TaskManager) handleInputMode(ev termbox.Event) bool {
	switch ev.Key {
	case termbox.KeyEsc:
		tm.inputMode = ""
		tm.inputStep = 0
		tm.inputTitle = ""
		tm.inputPriority = ""
	case termbox.KeyEnter:
		if tm.inputStep == 0 {
			// Move to priority input
			if tm.inputTitle == "" {
				tm.statusMsg = "Task title cannot be empty"
				return false
			}
			tm.inputStep = 1
		} else {
			// Create the task
			priority := 50
			if tm.inputPriority != "" {
				if p, err := strconv.Atoi(tm.inputPriority); err == nil {
					if p >= 1 && p <= 100 {
						priority = p
					}
				}
			}

			var parentID *int
			if tm.inputMode == "addsubtask" && len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
				currentTask := tm.flatView[tm.currentIndex]
				parentID = &currentTask.ID
			}

			err := tm.addTask(tm.inputTitle, priority, parentID)
			if err != nil {
				tm.statusMsg = fmt.Sprintf("Error adding task: %v", err)
			}

			tm.inputMode = ""
			tm.inputStep = 0
			tm.inputTitle = ""
			tm.inputPriority = ""
		}
	case termbox.KeyBackspace, termbox.KeyBackspace2:
		if tm.inputStep == 0 {
			if len(tm.inputTitle) > 0 {
				tm.inputTitle = tm.inputTitle[:len(tm.inputTitle)-1]
			}
		} else {
			if len(tm.inputPriority) > 0 {
				tm.inputPriority = tm.inputPriority[:len(tm.inputPriority)-1]
			}
		}
	case termbox.KeySpace:
		if tm.inputStep == 0 {
			tm.inputTitle += " "
		} else {
			tm.inputPriority += " "
		}
	default:
		if ev.Ch != 0 {
			if tm.inputStep == 0 {
				tm.inputTitle += string(ev.Ch)
			} else {
				tm.inputPriority += string(ev.Ch)
			}
		}
	}
	return false
}

// handleEditMode handles input in edit mode and returns true if should quit
func (tm *TaskManager) handleEditMode(ev termbox.Event) bool {
	switch ev.Key {
	case termbox.KeyEsc:
		tm.editMode = false
		tm.editBuffer = ""
	case termbox.KeyEnter:
		if len(tm.flatView) > 0 && tm.currentIndex < len(tm.flatView) {
			task := tm.flatView[tm.currentIndex]
			parts := strings.SplitN(tm.editBuffer, ":", 2)
			if len(parts) == 2 {
				title := strings.TrimSpace(parts[0])
				priority, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil {
					priority = task.Priority
				}
				if priority < 1 {
					priority = 1
				}
				if priority > 100 {
					priority = 100
				}
				tm.updateTask(task.ID, title, priority)
			}
		}
		tm.editMode = false
		tm.editBuffer = ""
	case termbox.KeyBackspace, termbox.KeyBackspace2:
		if len(tm.editBuffer) > 0 {
			tm.editBuffer = tm.editBuffer[:len(tm.editBuffer)-1]
		}
	default:
		if ev.Ch != 0 {
			tm.editBuffer += string(ev.Ch)
		}
	}
	return false
}

// promptAddTask is now deprecated - keeping for compatibility but not used
func (tm *TaskManager) promptAddTask(isSubtask bool) {
	// This function is no longer used - input is now inline
}

// Close closes the database connection
func (tm *TaskManager) Close() {
	if tm.db != nil {
		tm.db.Close()
	}
}

// Run starts the task manager
func (tm *TaskManager) Run() {
	defer tm.Close()

	err := termbox.Init()
	if err != nil {
		log.Fatal("Failed to initialize termbox:", err)
	}
	defer termbox.Close()

	for {
		tm.render()
		if tm.handleInput() {
			break
		}
	}
}

func main() {
	tm := NewTaskManager()
	tm.Run()
}

# nf - enfoque

**Enfoque/nf** is a terminal-based hierarchical task manager written in Go, using `termbox-go` for UI and `SQLite` for data persistence. Currently just a basic prototype built for testing out my ideal structure and out of frustration with trying to make other solutions do what I wanted. Needs to be refactored.

## Features

- Nested tasks and subtasks
- Priority-based sorting
- Keyboard-driven interface (vim-style navigation)
- Persistent storage with SQLite

## Controls

- `j/k`: Navigate
- `Shift+j/k`: Adjust priority
- `a`: Add task
- `s`: Add subtask
- `e`: Edit task
- `d`: Delete task
- `Space`: Expand/collapse
- `q`: Quit

## Build & Run

```bash
go build
./termtask
```
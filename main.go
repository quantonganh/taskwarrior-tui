package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	commandTask = "task"

	pageMain       = "main"
	pageNewTask    = "newTask"
	pageDeleteTask = "deleteTask"

	buttonCancel = "Cancel"
	buttonDelete = "Delete"

	numberOfColumns = 7

	layout = "20060102T150405Z"

	priorityHigh   = "H"
	priorityMedium = "M"
	priorityLow    = "L"
)

type Task struct {
	ID          int     `json:"id"`
	Age         string  `json:"age"`
	Description string  `json:"description"`
	Due         string  `json:"due"`
	Entry       string  `json:"entry"`
	Modified    string  `json:"modified"`
	Priority    string  `json:"priority"`
	Project     string  `json:"project"`
	Status      string  `json:"status"`
	UUID        string  `json:"uuid"`
	Urgency     float32 `json:"urgency"`
}

var (
	taskHeaders        = []string{"ID", "Age", "P", "Project", "Due", "Description", "Urg"}
	commandLogTextView = tview.NewTextView()
)

func main() {
	app := tview.NewApplication()

	taskTable := tview.NewTable()
	taskTable.SetTitle("Tasks").SetBorder(true)
	taskTable.SetSelectable(true, false)
	for col := 0; col < numberOfColumns; col++ {
		taskTable.SetCell(0, col, tview.NewTableCell(taskHeaders[col]).SetSelectable(false))
	}
	taskTable.SetFixed(2, 0)

	pendingTasks := runCommand("+PENDING", "export")
	var tasks []Task
	if err := json.Unmarshal(pendingTasks, &tasks); err != nil {
		panic(err)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].Urgency > tasks[j].Urgency
	})

	longestWidthOfProject := 0
	longestWidthOfDescription := 0
	for _, task := range tasks {
		if len(task.Project) > longestWidthOfProject {
			longestWidthOfProject = len(task.Project)
		}

		if len(task.Description) > longestWidthOfDescription {
			longestWidthOfDescription = len(task.Description)
		}
	}

	taskSeparators := []string{"--", "---", "--", strings.Repeat("-", longestWidthOfProject), "---", strings.Repeat("-", longestWidthOfDescription), strings.Repeat("-", 5)}
	for col := 0; col < numberOfColumns; col++ {
		taskTable.SetCell(1, col, tview.NewTableCell(taskSeparators[col]).SetSelectable(false))
	}

	for _, task := range tasks {
		insertRow(taskTable, task)
	}
	taskTable.Select(2, 0)

	commandLogTextView.SetTitle("Command Log").SetBorder(true)

	descriptionTextView := tview.NewTextView()
	descriptionTextView.SetTitle("Description").SetBorder(true)

	if taskTable.GetRowCount() > 1 {
		taskDesc := runCommand(taskTable.GetCell(1, 0).Text)
		descriptionTextView.SetText(string(taskDesc))
	}

	pages := tview.NewPages()

	newTaskInputField := tview.NewInputField()
	newTaskInputField.SetTitle("New Task")
	newTaskInputField.
		SetFieldWidth(100).
		SetAcceptanceFunc(tview.InputFieldMaxLength(100))
	newTaskInputField.SetBorder(true)
	newTaskInputField.SetDoneFunc(func(key tcell.Key) {

	})

	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false)
	}

	deleteTaskModal := tview.NewModal()
	deleteTaskModal.AddButtons([]string{buttonCancel, buttonDelete})

	taskTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'a':
			newTaskInputField.SetDoneFunc(func(key tcell.Key) {
				switch key {
				case tcell.KeyESC:
					pages.HidePage(pageNewTask)
					app.SetFocus(taskTable)
				case tcell.KeyEnter:
					desc := newTaskInputField.GetText()
					args := []string{"add"}
					args = append(args, splitFields(desc)...)
					out := runCommand(args...)
					lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
					if len(lines) >= 1 {
						var tasks []Task
						if err := json.Unmarshal(runCommand(getTaskID(lines[0]), "export"), &tasks); err != nil {
							panic(err)
						}
						insertRow(taskTable, tasks[0])
					}

					pages.HidePage(pageNewTask)
					app.SetFocus(taskTable)
				}
			})
			pages.AddPage(pageNewTask, modal(newTaskInputField, 100, 3), true, true)
		case 'e':
			row, _ := taskTable.GetSelection()
			_ = runCommand(taskTable.GetCell(row, 0).Text, "edit")
		case 'd':
			row, _ := taskTable.GetSelection()
			out := runCommand(taskTable.GetCell(row, 0).Text, "done")
			taskTable.RemoveRow(row)
			fmt.Fprintln(commandLogTextView, string(out))
		case 'x':
			row, _ := taskTable.GetSelection()
			taskID := taskTable.GetCell(row, 0).Text
			taskDesc := taskTable.GetCell(row, 5).Text

			deleteTaskModal.SetText(fmt.Sprintf("Delete task %s '%s'?", taskID, taskDesc)).
				SetFocus(0).
				SetDoneFunc(func(buttonIndex int, buttonLabel string) {
					switch buttonLabel {
					case buttonCancel:
						pages.HidePage(pageDeleteTask)
						app.SetFocus(taskTable)

					case buttonDelete:
						taskTable.RemoveRow(row)
						_ = runCommand("rc.confirmation=off", taskID, "delete")
						pages.HidePage(pageDeleteTask)
						if taskTable.GetRowCount() > 0 {
							app.SetFocus(taskTable)
						}
					}
				}).
				SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					switch event.Key() {
					case tcell.KeyESC:
						pages.HidePage(pageDeleteTask)
						app.SetFocus(taskTable)
					}
					return event
				})
			pages.ShowPage(pageDeleteTask)
		}
		return event
	})

	taskTable.SetSelectionChangedFunc(func(row, col int) {
		if row == 0 {
			descriptionTextView.SetText("")
		} else {
			taskID := taskTable.GetCell(row, 0).Text
			out := runCommand(taskID)
			descriptionTextView.SetText(string(out))
		}
	})
	taskTable.SetWrapSelection(true, false)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case '1':
			app.SetFocus(taskTable)
		case '2':
			app.SetFocus(descriptionTextView)
		default:
			return event
		}
		return nil
	})

	mainFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(taskTable, 0, 1, false).
			AddItem(commandLogTextView, 5, 1, false), 0, 1, false).
		AddItem(descriptionTextView, 0, 1, false)
	pages.
		AddPage(pageMain, mainFlex, true, true).
		AddPage(pageDeleteTask, deleteTaskModal, true, false)
	if err := app.SetRoot(pages, true).SetFocus(taskTable).Run(); err != nil {
		panic(err)
	}
}

func runCommand(args ...string) []byte {
	out, err := exec.Command(commandTask, args...).CombinedOutput()
	if err == nil {
		return out
	}
	fmt.Fprintln(commandLogTextView, string(out))
	return nil
}

func splitFields(s string) []string {
	inQuotes := false
	return strings.FieldsFunc(s, func(r rune) bool {
		if r == '\'' {
			inQuotes = !inQuotes
		}
		return r == ' ' && !inQuotes
	})
}

func getTaskID(s string) string {
	re := regexp.MustCompile(`Created task (\d+).`)
	match := re.FindStringSubmatch(s)
	if match != nil {
		return match[1]
	}
	return ""
}

var priorityToColor = map[string]tcell.Color{
	priorityHigh:   tcell.ColorRed,
	priorityMedium: tcell.Color250,
	priorityLow:    tcell.Color245,
}

func insertRow(table *tview.Table, task Task) {
	rowCount := table.GetRowCount()
	table = table.InsertRow(rowCount)
	setCell(table, rowCount, 0, task.Priority, strconv.Itoa(task.ID))

	age, err := time.Parse(layout, task.Entry)
	if err == nil {
		setCell(table, rowCount, 1, task.Priority, fmtDuration(time.Now().Sub(age)))
	}

	setCell(table, rowCount, 2, task.Priority, task.Priority)
	setCell(table, rowCount, 3, task.Priority, task.Project)

	if task.Due == "" {
		table.SetCellSimple(rowCount, 4, "")
	} else {
		now := time.Now()
		due, err := time.Parse(layout, task.Due)
		if err == nil {
			if due.After(now) {
				setCell(table, rowCount, 4, task.Priority, fmtDuration(due.Sub(now)))
			} else {
				setCell(table, rowCount, 4, task.Priority, fmt.Sprintf("-%s", fmtDuration(now.Sub(due))))
			}
		}
	}

	setCell(table, rowCount, 5, task.Priority, task.Description)
	setCell(table, rowCount, 6, task.Priority, fmt.Sprintf("%.2f", task.Urgency))
}

func setCell(table *tview.Table, row, col int, priority string, text string) {
	if priority == "" {
		table.SetCellSimple(row, col, text)
	} else {
		table.SetCell(row, col, tview.NewTableCell(text).SetTextColor(priorityToColor[priority]))
	}
}

type unit struct {
	duration time.Duration
	label    string
}

var units = []unit{
	{
		duration: 365 * 24 * time.Hour,
		label:    "y",
	},
	{
		duration: 30 * 24 * time.Hour,
		label:    "mo",
	},
	{
		duration: 7 * 24 * time.Hour,
		label:    "w",
	},
	{
		duration: 24 * time.Hour,
		label:    "d",
	},
	{
		duration: time.Hour,
		label:    "h",
	},
	{
		duration: time.Minute,
		label:    "min",
	},
	{
		duration: time.Second,
		label:    "s",
	},
}

func fmtDuration(d time.Duration) string {
	for _, u := range units {
		q := int(d / u.duration)
		if q > 0 {
			return fmt.Sprintf("%d%s", q, u.label)
		}
	}
	return ""
}

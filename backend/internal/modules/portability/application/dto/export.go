package dto

// TodosCSVHeader is the column order of the todos.csv human-readable copy:
// identity, title, status, the due instant with its input timezone, and the
// historical timestamps. Optional timestamps stay empty when absent.
var TodosCSVHeader = []string{
	"id",
	"title",
	"status",
	"dueAtUtc",
	"timezoneAtInput",
	"createdAt",
	"completedAt",
	"deletedAt",
}

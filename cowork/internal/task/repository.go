package task

import "context"

// Repository keeps tasks, their links and their comments. Every
// method is scoped to an organization.
type Repository interface {
	// Create stores t with its links.
	Create(ctx context.Context, t Task) error
	// Get answers a task with its links.
	Get(ctx context.Context, org, id string) (Task, error)
	// List answers the tasks matching f, with their links, newest
	// first, at most limit.
	List(ctx context.Context, org string, f Filter, limit int) ([]Task, error)
	// Update replaces the stored task with t, its links included.
	Update(ctx context.Context, t Task) error
	AddComment(ctx context.Context, c Comment) error
	// Comments answers a task's comments, oldest first.
	Comments(ctx context.Context, org, task string) ([]Comment, error)
}

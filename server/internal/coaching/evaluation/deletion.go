package evaluation

import "context"

type DeleteUserDataCommand struct {
	OwnerUserID        string
	DeletionGeneration int64
}

type EvaluationDataDeleter interface {
	DeleteUserData(context.Context, DeleteUserDataCommand) error
}

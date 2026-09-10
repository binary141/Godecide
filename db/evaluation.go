package db

import (
	"context"
	"encoding/json"
	"time"
)

// Evaluation records a single call to /evaluate against a deployment: the
// inputs it was called with, the outputs it produced (or the error it
// failed with), and when it happened. This lets callers ask "what did
// deployment X return last Tuesday for these inputs" after the fact.
type Evaluation struct {
	ID           int64           `json:"id" db:"id"`
	DeploymentID int64           `json:"deploymentId" db:"deployment_id"`
	Inputs       json.RawMessage `json:"inputs" db:"inputs"`
	Outputs      json.RawMessage `json:"outputs,omitempty" db:"outputs"`
	Error        *string         `json:"error,omitempty" db:"error"`
	CreatedAt    time.Time       `json:"createdAt" db:"created_at"`
}

// RecordEvaluation logs one evaluation call. outputs and evalErr are
// mutually exclusive: pass whichever the evaluation actually produced.
func RecordEvaluation(ctx context.Context, deploymentID int64, inputs map[string]any, outputs map[string]any, evalErr error) error {
	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return err
	}

	var outputsJSON []byte
	if outputs != nil {
		outputsJSON, err = json.Marshal(outputs)
		if err != nil {
			return err
		}
	}

	var errMsg *string
	if evalErr != nil {
		msg := evalErr.Error()
		errMsg = &msg
	}

	_, err = DB.ExecContext(
		ctx,
		`INSERT INTO evaluations (deployment_id, inputs, outputs, error) VALUES ($1, $2, $3, $4)`,
		deploymentID, inputsJSON, outputsJSON, errMsg,
	)
	return err
}

const (
	defaultEvaluationListLimit = 20
	maxEvaluationListLimit     = 100
)

// ListEvaluations returns a page of evaluation history for a deployment,
// newest first, along with the total number of evaluations recorded for it.
func ListEvaluations(ctx context.Context, deploymentID int64, limit, offset int) ([]Evaluation, int, error) {
	evaluations := []Evaluation{}
	err := DB.SelectContext(
		ctx,
		&evaluations,
		`SELECT id, deployment_id, inputs, outputs, error, created_at
		 FROM evaluations
		 WHERE deployment_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		deploymentID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := DB.GetContext(ctx, &total, `SELECT COUNT(*) FROM evaluations WHERE deployment_id = $1`, deploymentID); err != nil {
		return nil, 0, err
	}

	return evaluations, total, nil
}

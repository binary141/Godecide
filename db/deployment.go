package db

import (
	"context"
	"time"
)

// Deployment is a single ingested DMN file, stored verbatim so it can be
// re-parsed and evaluated later. Deployments are append-only: redeploying a
// DMN with the same name simply inserts another row.
type Deployment struct {
	ID         int64     `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	Namespace  string    `json:"namespace" db:"namespace"`
	DMNVersion string    `json:"dmnVersion" db:"dmn_version"`
	XML        string    `json:"xml,omitempty" db:"xml"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
}

func CreateDeployment(ctx context.Context, name, namespace, dmnVersion, xml string) (Deployment, error) {
	var d Deployment
	err := DB.QueryRowxContext(
		ctx,
		`INSERT INTO deployments (name, namespace, dmn_version, xml)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, namespace, dmn_version, xml, created_at`,
		name, namespace, dmnVersion, xml,
	).StructScan(&d)
	return d, err
}

// ListDeployments returns a page of deployments newest-first, omitting the
// XML body (which can be large) so the listing stays cheap, along with the
// total number of deployments so callers can tell when they've paged
// through everything.
func ListDeployments(ctx context.Context, limit, offset int) ([]Deployment, int, error) {
	deployments := []Deployment{}
	err := DB.SelectContext(
		ctx,
		&deployments,
		`SELECT id, name, namespace, dmn_version, created_at FROM deployments ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := DB.GetContext(ctx, &total, `SELECT COUNT(*) FROM deployments`); err != nil {
		return nil, 0, err
	}

	return deployments, total, nil
}

func GetDeployment(ctx context.Context, id int64) (Deployment, error) {
	var d Deployment
	err := DB.GetContext(
		ctx,
		&d,
		`SELECT id, name, namespace, dmn_version, xml, created_at FROM deployments WHERE id = $1`,
		id,
	)
	return d, err
}

// DeleteDeployment removes a deployment by id, reporting whether a row was
// actually deleted so callers can distinguish "gone" from "never existed".
func DeleteDeployment(ctx context.Context, id int64) (bool, error) {
	res, err := DB.ExecContext(ctx, `DELETE FROM deployments WHERE id = $1`, id)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

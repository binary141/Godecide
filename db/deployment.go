package db

import (
	"context"
	"time"
)

// Deployment is a single ingested DMN file, stored verbatim so it can be
// re-parsed and evaluated later. Deployments are append-only: redeploying a
// DMN with the same name inserts another row and bumps Version, mirroring
// how engines like Camunda version decisions by key.
type Deployment struct {
	ID         int64     `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	Namespace  string    `json:"namespace" db:"namespace"`
	DMNVersion string    `json:"dmnVersion" db:"dmn_version"`
	Version    int       `json:"version" db:"version"`
	XML        string    `json:"xml,omitempty" db:"xml"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
}

// CreateDeployment inserts a new deployment with the next version number
// for its name (1 if this is the first deployment of that name). The
// version is computed and inserted inside a transaction guarded by a
// transaction-scoped advisory lock keyed on the name, so concurrent
// deploys of the same name can't race to the same version number.
func CreateDeployment(ctx context.Context, name, namespace, dmnVersion, xml string) (Deployment, error) {
	tx, err := DB.BeginTxx(ctx, nil)
	if err != nil {
		return Deployment{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, name); err != nil {
		return Deployment{}, err
	}

	var nextVersion int
	if err := tx.GetContext(ctx, &nextVersion, `SELECT COALESCE(MAX(version), 0) + 1 FROM deployments WHERE name = $1`, name); err != nil {
		return Deployment{}, err
	}

	var d Deployment
	err = tx.QueryRowxContext(
		ctx,
		`INSERT INTO deployments (name, namespace, dmn_version, version, xml)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, namespace, dmn_version, version, xml, created_at`,
		name, namespace, dmnVersion, nextVersion, xml,
	).StructScan(&d)
	if err != nil {
		return Deployment{}, err
	}

	if err := tx.Commit(); err != nil {
		return Deployment{}, err
	}

	return d, nil
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
		`SELECT id, name, namespace, dmn_version, version, created_at FROM deployments ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
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
		`SELECT id, name, namespace, dmn_version, version, xml, created_at FROM deployments WHERE id = $1`,
		id,
	)
	return d, err
}

// GetLatestDeploymentByName returns the highest-versioned deployment with
// the given name.
func GetLatestDeploymentByName(ctx context.Context, name string) (Deployment, error) {
	var d Deployment
	err := DB.GetContext(
		ctx,
		&d,
		`SELECT id, name, namespace, dmn_version, version, xml, created_at FROM deployments WHERE name = $1 ORDER BY version DESC LIMIT 1`,
		name,
	)
	return d, err
}

// GetDeploymentByNameVersion returns a specific historical version of a
// named deployment.
func GetDeploymentByNameVersion(ctx context.Context, name string, version int) (Deployment, error) {
	var d Deployment
	err := DB.GetContext(
		ctx,
		&d,
		`SELECT id, name, namespace, dmn_version, version, xml, created_at FROM deployments WHERE name = $1 AND version = $2`,
		name, version,
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

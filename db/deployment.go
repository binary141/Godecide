package db

import "time"

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

func CreateDeployment(name, namespace, dmnVersion, xml string) (Deployment, error) {
	var d Deployment
	err := DB.QueryRowx(
		`INSERT INTO deployments (name, namespace, dmn_version, xml)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, namespace, dmn_version, xml, created_at`,
		name, namespace, dmnVersion, xml,
	).StructScan(&d)
	return d, err
}

// ListDeployments returns deployments newest-first, omitting the XML body
// (which can be large) so the listing stays cheap.
func ListDeployments() ([]Deployment, error) {
	deployments := []Deployment{}
	err := DB.Select(
		&deployments,
		`SELECT id, name, namespace, dmn_version, created_at FROM deployments ORDER BY created_at DESC`,
	)
	return deployments, err
}

func GetDeployment(id int64) (Deployment, error) {
	var d Deployment
	err := DB.Get(
		&d,
		`SELECT id, name, namespace, dmn_version, xml, created_at FROM deployments WHERE id = $1`,
		id,
	)
	return d, err
}

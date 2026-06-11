// Package registry tracks which inference nodes currently serve which
// models. Nodes heartbeat over the authenticated API; entries older than
// the freshness TTL are ignored by the router, so a dead node simply
// disappears from the catalog.
package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModelAd is one model a node advertises, with the mesh URL serving it.
type ModelAd struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// Heartbeat is one node report.
type Heartbeat struct {
	Subject string    // operator identity (from auth)
	Node    string    // node name (e.g. "newton")
	Models  []ModelAd `json:"models"`
}

// PG is the Postgres-backed registry.
type PG struct {
	pool *pgxpool.Pool
}

func NewPG(pool *pgxpool.Pool) *PG { return &PG{pool: pool} }

// Upsert records a heartbeat: advertised models are refreshed, models the
// node no longer advertises are removed.
func (p *PG) Upsert(ctx context.Context, hb Heartbeat) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ids := make([]string, len(hb.Models))
	for i, m := range hb.Models {
		ids[i] = m.ID
		_, err := tx.Exec(ctx, `
			INSERT INTO node_models (node, subject, model, url, last_seen)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (node, model)
			DO UPDATE SET url = $4, subject = $2, last_seen = now()`,
			hb.Node, hb.Subject, m.ID, m.URL)
		if err != nil {
			return fmt.Errorf("upserting %s/%s: %w", hb.Node, m.ID, err)
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM node_models WHERE node = $1 AND NOT (model = ANY($2))`,
		hb.Node, ids); err != nil {
		return fmt.Errorf("pruning %s: %w", hb.Node, err)
	}
	return tx.Commit(ctx)
}

// Lookup returns the freshest URL serving model, if any node is alive.
func (p *PG) Lookup(ctx context.Context, model string, ttl time.Duration) (string, bool, error) {
	var url string
	err := p.pool.QueryRow(ctx, `
		SELECT url FROM node_models
		WHERE model = $1 AND last_seen > now() - $2::interval
		ORDER BY last_seen DESC LIMIT 1`,
		model, ttl.String(),
	).Scan(&url)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return url, true, nil
}

// NodeStatus is one node as its operator sees it.
type NodeStatus struct {
	Node     string    `json:"node"`
	Models   []string  `json:"models"`
	LastSeen time.Time `json:"last_seen"`
	Online   bool      `json:"online"`
}

// NodesBySubject lists the caller's nodes with liveness per ttl.
func (p *PG) NodesBySubject(ctx context.Context, subject string, ttl time.Duration) ([]NodeStatus, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT node,
		       array_agg(model ORDER BY model),
		       max(last_seen),
		       max(last_seen) > now() - $2::interval
		FROM node_models
		WHERE subject = $1
		GROUP BY node ORDER BY node`,
		subject, ttl.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []NodeStatus
	for rows.Next() {
		var n NodeStatus
		if err := rows.Scan(&n.Node, &n.Models, &n.LastSeen, &n.Online); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// AllNodes lists every node with liveness per ttl (admin surface).
func (p *PG) AllNodes(ctx context.Context, ttl time.Duration) ([]NodeStatus, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT node,
		       array_agg(model ORDER BY model),
		       max(last_seen),
		       max(last_seen) > now() - $1::interval
		FROM node_models
		GROUP BY node ORDER BY node`,
		ttl.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []NodeStatus
	for rows.Next() {
		var n NodeStatus
		if err := rows.Scan(&n.Node, &n.Models, &n.LastSeen, &n.Online); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// Models lists distinct live model ids.
func (p *PG) Models(ctx context.Context, ttl time.Duration) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT model FROM node_models
		WHERE last_seen > now() - $1::interval ORDER BY model`,
		ttl.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

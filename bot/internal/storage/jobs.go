package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// JobStatus is the state of an analysis job.
type JobStatus string

// The states of a job: it waits in the queue, a worker parses its demo, and
// it ends done or failed.
const (
	JobQueued  JobStatus = "queued"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// ReasonInterrupted is the reason of a job the bot died on as many times as
// RecoverJobs allows.
const ReasonInterrupted = "interrupted"

// ErrJobExists means the user has a job for the match in the queue already.
var ErrJobExists = errors.New("storage: the user has a job for the match in the queue")

// Job is a demo link a user sent and what became of it.
type Job struct {
	ID         int64
	TelegramID int64
	MatchID    string
	// DemoURL is the signed link to the demo, a credential for a few
	// minutes; "" once the job is over.
	DemoURL     string
	LinkExpires time.Time // when the link runs out, zero if it does not tell
	Language    string    // of the bot, "ru" or "en"
	Status      JobStatus
	Reason      string // why the job failed
	// StatusMessageID is the message that shows the status of the job, 0
	// until the bot sends it.
	StatusMessageID int
	// Attempts is how many times a worker took the job.
	Attempts   int
	CreatedAt  time.Time
	StartedAt  time.Time // zero until a worker takes the job
	FinishedAt time.Time // zero until it ends
}

// NewJob is a job to add to the queue.
type NewJob struct {
	TelegramID  int64
	MatchID     string
	DemoURL     string
	LinkExpires time.Time // zero if unknown
	Language    string
}

// QueueState is the queue as a job sees it: the jobs that run and how many
// wait ahead of it.
type QueueState struct {
	Running []time.Time // when the running jobs started, earliest first
	Ahead   int
}

const jobColumns = `id, telegram_id, match_id, demo_url, link_expires_at, language, status, reason,
	status_message_id, attempts, created_at, started_at, finished_at`

type scanner interface{ Scan(dest ...any) error }

func scanJob(row scanner) (Job, error) {
	var (
		j                                     Job
		url, reason                           sql.NullString
		expires, messageID, started, finished sql.NullInt64
		created                               int64
		status                                string
	)
	err := row.Scan(&j.ID, &j.TelegramID, &j.MatchID, &url, &expires, &j.Language, &status, &reason,
		&messageID, &j.Attempts, &created, &started, &finished)
	if err != nil {
		return Job{}, err
	}
	j.DemoURL, j.Reason, j.Status = url.String, reason.String, JobStatus(status)
	j.LinkExpires, j.StartedAt, j.FinishedAt = unixOrZero(expires), unixOrZero(started), unixOrZero(finished)
	j.StatusMessageID = int(messageID.Int64)
	j.CreatedAt = time.Unix(created, 0).UTC()
	return j, nil
}

// unixOrZero converts unix seconds, NULL being the zero time.
func unixOrZero(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0).UTC()
}

// nullUnix stores t as unix seconds, the zero time as NULL.
func nullUnix(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: t.Unix(), Valid: !t.IsZero()}
}

// AddJob adds a job at the end of the queue. First admit sees the queue the
// job would join, all the queued jobs being ahead of it, and may refuse the
// job by returning an error, which AddJob returns as is. The check and the
// insert are one transaction, so the queue cannot change in between. A user
// with a job for the match in the queue already gets ErrJobExists.
func (s *Store) AddJob(ctx context.Context, j NewJob, now time.Time, admit func(QueueState) error) (_ Job, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM jobs
		WHERE telegram_id = ? AND match_id = ? AND status IN ('queued', 'running'))`, j.TelegramID, j.MatchID).Scan(&exists)
	if err != nil {
		return Job{}, fmt.Errorf("select active job: %w", err)
	}
	if exists {
		return Job{}, ErrJobExists
	}
	state, err := queueState(ctx, tx, 0)
	if err != nil {
		return Job{}, err
	}
	if err := admit(state); err != nil {
		return Job{}, err
	}

	row := tx.QueryRowContext(ctx, `INSERT INTO jobs (telegram_id, match_id, demo_url, link_expires_at, language, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'queued', ?) RETURNING `+jobColumns,
		j.TelegramID, j.MatchID, j.DemoURL, nullUnix(j.LinkExpires), j.Language, now.Unix())
	job, err := scanJob(row)
	if err != nil {
		return Job{}, fmt.Errorf("insert job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}
	return job, nil
}

// queryer is what queueState needs of a database or a transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queueState reads the running jobs and counts the queued jobs ahead of the
// job with the ID; with 0, all the queued jobs.
func queueState(ctx context.Context, q queryer, id int64) (QueueState, error) {
	rows, err := q.QueryContext(ctx, `SELECT started_at FROM jobs WHERE status = 'running' ORDER BY started_at, id`)
	if err != nil {
		return QueueState{}, fmt.Errorf("select running jobs: %w", err)
	}
	defer rows.Close()
	var state QueueState
	for rows.Next() {
		var started sql.NullInt64
		if err := rows.Scan(&started); err != nil {
			return QueueState{}, fmt.Errorf("scan running job: %w", err)
		}
		state.Running = append(state.Running, unixOrZero(started))
	}
	if err := rows.Err(); err != nil {
		return QueueState{}, fmt.Errorf("select running jobs: %w", err)
	}
	query, args := `SELECT COUNT(*) FROM jobs WHERE status = 'queued'`, []any{}
	if id != 0 {
		query, args = query+` AND id < ?`, append(args, id)
	}
	if err := q.QueryRowContext(ctx, query, args...).Scan(&state.Ahead); err != nil {
		return QueueState{}, fmt.Errorf("count queued jobs: %w", err)
	}
	return state, nil
}

// QueueAhead returns the queue as the queued job with the ID sees it: the
// running jobs and the queued ones ahead of it.
func (s *Store) QueueAhead(ctx context.Context, id int64) (QueueState, error) {
	return queueState(ctx, s.db, id)
}

// ActiveJobs returns the jobs of the user that wait or run, oldest first.
func (s *Store) ActiveJobs(ctx context.Context, telegramID int64) ([]Job, error) {
	return s.jobs(ctx, `WHERE telegram_id = ? AND status IN ('queued', 'running') ORDER BY id`, telegramID)
}

// ActiveJobIDs returns the IDs of all the jobs that wait or run, in order.
func (s *Store) ActiveJobIDs(ctx context.Context) ([]int64, error) {
	jobs, err := s.jobs(ctx, `WHERE status IN ('queued', 'running') ORDER BY id`)
	ids := make([]int64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids, err
}

// QueuedJobIDs returns the IDs of the queued jobs in the order of the queue.
func (s *Store) QueuedJobIDs(ctx context.Context) ([]int64, error) {
	jobs, err := s.jobs(ctx, `WHERE status = 'queued' ORDER BY id`)
	ids := make([]int64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids, err
}

func (s *Store) jobs(ctx context.Context, where string, args ...any) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM jobs `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("select jobs: %w", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select jobs: %w", err)
	}
	return out, nil
}

// Job returns the job with the ID, ErrNotFound if there is none.
func (s *Store) Job(ctx context.Context, id int64) (Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("select job: %w", err)
	}
	return j, nil
}

// ClaimJob takes the oldest queued job for a worker: it becomes running, and
// its attempt is counted. A job for a match that another job is parsing
// waits, and the next one is taken. ok is false if no job can be taken.
func (s *Store) ClaimJob(ctx context.Context, now time.Time) (_ Job, ok bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	row := tx.QueryRowContext(ctx, `UPDATE jobs SET status = 'running', attempts = attempts + 1, started_at = ?
		WHERE id = (SELECT q.id FROM jobs q
			WHERE q.status = 'queued'
			AND NOT EXISTS (SELECT 1 FROM jobs r WHERE r.status = 'running' AND r.match_id = q.match_id)
			ORDER BY q.id LIMIT 1)
		RETURNING `+jobColumns, now.Unix())
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("commit: %w", err)
	}
	return j, true, nil
}

// FinishJob ends a job as done and wipes its link.
func (s *Store) FinishJob(ctx context.Context, id int64, now time.Time) error {
	return s.endJob(ctx, id, JobDone, "", now)
}

// FailJob ends a job as failed for the reason and wipes its link.
func (s *Store) FailJob(ctx context.Context, id int64, reason string, now time.Time) error {
	return s.endJob(ctx, id, JobFailed, reason, now)
}

func (s *Store) endJob(ctx context.Context, id int64, status JobStatus, reason string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, reason = ?, demo_url = NULL, finished_at = ? WHERE id = ?`,
		string(status), nullString(reason), now.Unix(), id)
	if err != nil {
		return fmt.Errorf("end job: %w", err)
	}
	return nil
}

// RequeueJob puts a running job back at its place in the queue and takes
// back its attempt: the bot is stopping, and the demo is not to blame.
func (s *Store) RequeueJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'queued', attempts = MAX(attempts - 1, 0), started_at = NULL
		WHERE id = ? AND status = 'running'`, id)
	if err != nil {
		return fmt.Errorf("requeue job: %w", err)
	}
	return nil
}

// RecoverJobs handles the jobs a bot that died left running, the bot
// killed for memory for example: a job taken maxAttempts times fails with
// ReasonInterrupted, the rest go back to their places in the queue. It
// returns the IDs of the jobs it changed.
func (s *Store) RecoverJobs(ctx context.Context, maxAttempts int, now time.Time) (_ []int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx, &err)

	rows, err := tx.QueryContext(ctx, `SELECT id FROM jobs WHERE status = 'running' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select running jobs: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan running job: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select running jobs: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', reason = ?, demo_url = NULL, finished_at = ?
		WHERE status = 'running' AND attempts >= ?`, ReasonInterrupted, now.Unix(), maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("fail interrupted jobs: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'queued', started_at = NULL WHERE status = 'running'`)
	if err != nil {
		return nil, fmt.Errorf("requeue interrupted jobs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return ids, nil
}

// SetJobMessage remembers the message that shows the status of the job.
func (s *Store) SetJobMessage(ctx context.Context, id int64, messageID int) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET status_message_id = ? WHERE id = ?`, messageID, id); err != nil {
		return fmt.Errorf("set job message: %w", err)
	}
	return nil
}

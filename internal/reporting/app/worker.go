// Package app — background worker that drains the report-export queue.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Worker polls REQUESTED report exports, generates their files via the
// Generator port, and updates their status. Multiple Worker instances are safe
// to run concurrently because Repository.ClaimNext relies on row-level locking
// (FOR UPDATE SKIP LOCKED) to hand each job to exactly one worker.
type Worker struct {
	repo     Repository
	gen      Generator
	logger   *slog.Logger
	interval time.Duration
}

// NewWorker wires a Worker. When pollInterval is non-positive it defaults to
// five seconds.
func NewWorker(repo Repository, gen Generator, logger *slog.Logger, pollInterval time.Duration) *Worker {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{repo: repo, gen: gen, logger: logger, interval: pollInterval}
}

// Run drains the queue until ctx is cancelled. It processes jobs back-to-back
// while work is available, then sleeps for the poll interval before checking
// again. Run blocks; callers typically launch it in a goroutine and cancel ctx
// for graceful shutdown.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("reporting worker started", slog.Duration("pollInterval", w.interval))
	for {
		// Drain everything currently claimable before sleeping.
		for {
			if ctx.Err() != nil {
				w.logger.Info("reporting worker stopped")
				return
			}
			processed, err := w.processOne(ctx)
			if err != nil {
				w.logger.Error("reporting worker claim failed", slog.Any("error", err))
				break
			}
			if !processed {
				break
			}
		}

		select {
		case <-ctx.Done():
			w.logger.Info("reporting worker stopped")
			return
		case <-time.After(w.interval):
		}
	}
}

// processOne claims at most one job and runs it to completion. It reports
// whether a job was processed so Run can keep draining while work remains.
func (w *Worker) processOne(ctx context.Context) (bool, error) {
	export, err := w.repo.ClaimNext(ctx)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	log := w.logger.With(slog.String("reportId", export.ID), slog.String("reportType", string(export.ReportType)))
	log.Info("reporting job claimed")

	filePath, genErr := w.gen.Generate(ctx, export)
	if genErr != nil {
		log.Error("reporting job generation failed", slog.Any("error", genErr))
		if markErr := w.repo.MarkFailed(ctx, export.ID, genErr.Error()); markErr != nil {
			log.Error("reporting job mark-failed failed", slog.Any("error", markErr))
		}
		// The job was handled (moved to FAILED); keep draining the queue.
		return true, nil
	}

	if markErr := w.repo.MarkCompleted(ctx, export.ID, filePath); markErr != nil {
		log.Error("reporting job mark-completed failed", slog.Any("error", markErr))
		return true, nil
	}
	log.Info("reporting job completed", slog.String("filePath", filePath))
	return true, nil
}

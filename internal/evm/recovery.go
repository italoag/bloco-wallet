package evm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RecoveryTracker interface {
	TrackTransaction(context.Context, string, uint64, time.Time) (TrackingResult, error)
	Rebroadcast(context.Context, string) (ExecutionResult, error)
}

type RecoveryTrackerFactory func(context.Context, uint64) (RecoveryTracker, error)

type RecoverySupervisor struct {
	repository TransactionRepository
	factory    RecoveryTrackerFactory
	signingTTL time.Duration
}

func NewRecoverySupervisor(repository TransactionRepository, factory RecoveryTrackerFactory, signingTTL time.Duration) (*RecoverySupervisor, error) {
	if repository == nil || factory == nil || signingTTL <= 0 || signingTTL > 10*time.Minute {
		return nil, fmt.Errorf("EVM recovery configuration is invalid")
	}
	return &RecoverySupervisor{repository: repository, factory: factory, signingTTL: signingTTL}, nil
}

func (supervisor *RecoverySupervisor) RecoverOnce(ctx context.Context, limit int, now time.Time) error {
	if supervisor == nil || now.IsZero() {
		return invalidIntent("recovery supervisor")
	}
	records, err := supervisor.repository.ListRecoverableTransactions(ctx, limit)
	if err != nil {
		return err
	}
	var recoveryErrors []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(recoveryErrors, err)...)
		}
		if record.State == TransactionSigning {
			if !record.UpdatedAt.Add(supervisor.signingTTL).After(now) {
				if err := supervisor.repository.RecordSigningFailure(ctx, SigningFailureRequest{
					TransactionID: record.TransactionID, FailedAt: now.UTC(), ResultCode: "persistence_failed",
				}); err != nil {
					recoveryErrors = append(recoveryErrors, err)
				}
			}
			continue
		}
		if record.TransactionHash == ([32]byte{}) {
			continue
		}
		tracker, err := supervisor.factory(ctx, record.ChainID)
		if err != nil {
			recoveryErrors = append(recoveryErrors, err)
			continue
		}
		target := record.ConfirmationTarget
		if target == 0 {
			target = 1
		}
		tracking, err := tracker.TrackTransaction(ctx, record.TransactionID, target, now.UTC())
		if err != nil {
			recoveryErrors = append(recoveryErrors, err)
			continue
		}
		if record.BroadcastAttempts < 3 && (tracking.State == TransactionBroadcasting || tracking.State == TransactionBroadcastFailed || tracking.State == TransactionReorged) {
			if _, err := tracker.Rebroadcast(ctx, record.TransactionID); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}
	return errors.Join(recoveryErrors...)
}

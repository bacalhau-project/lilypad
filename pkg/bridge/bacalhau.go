package bridge

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/job"
	"github.com/filecoin-project/bacalhau/pkg/model"
	"github.com/filecoin-project/bacalhau/pkg/requester/publicapi"
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/ipfs/go-cid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/exp/maps"
)

const LilypadJobAnnotation string = "lilypad-job"

func init() {
	err := system.InitConfig()
	if err != nil {
		panic(err)
	}
}

// A JobRunner is a component that converts events into messages into the
// Bacalhau network.
type JobRunner interface {
	// Create starts a new Bacalhau job for the passed contract submission.
	Create(ctx context.Context, job ContractSubmittedEvent) (BacalhauJobRunningEvent, error)

	// FindCompleted queries the Bacalhau network for job statuses for the
	// passed jobs, and returns slices of jobs that have either completed
	// successfully (according to the network) or have failed.
	//
	// Any jobs still in progress are not returned. Any jobs that the network
	// does not seem to know about are considered failed.
	FindCompleted(ctx context.Context, jobs []BacalhauJobRunningEvent) ([]BacalhauJobCompletedEvent, []BacalhauJobFailedEvent)
}

type bacalhauRunner struct {
	Client *publicapi.RequesterAPIClient
}

// Create implements JobRunner
func (r *bacalhauRunner) Create(ctx context.Context, e ContractSubmittedEvent) (BacalhauJobRunningEvent, error) {
	job, err := model.NewJobWithSaneProductionDefaults()
	if err != nil {
		return nil, errors.Wrap(err, "error creating Bacalhau job")
	}

	job.Spec, err = e.Spec()
	if err != nil {
		return nil, errors.Wrap(err, "invalid job spec")
	}

	job.Spec.Annotations = append(job.Spec.Annotations,
		LilypadJobAnnotation,
		fmt.Sprintf("%s-%s", LilypadJobAnnotation, e.OrderId()), // TODO do some encryption thing here
	)
	job, err = r.Client.Submit(ctx, job)
	if err != nil {
		err = errors.Wrap(err, "error submitting Bacalhau job")
	}

	log.Ctx(ctx).Info().Stringer("id", e.OrderId()).Str("job", job.Metadata.ID).Msg("Created Bacalhau job")
	return e.JobCreated(job), err
}

// FindCompleted implements JobRunner
func (runner *bacalhauRunner) FindCompleted(ctx context.Context, jobs []BacalhauJobRunningEvent) ([]BacalhauJobCompletedEvent, []BacalhauJobFailedEvent) {
	log.Ctx(ctx).Debug().Int("jobs", len(jobs)).Msg("Looking at job states")

	completed := make([]BacalhauJobCompletedEvent, 0, len(jobs))
	failed := make([]BacalhauJobFailedEvent, 0, len(jobs))
	if len(jobs) <= 0 {
		return completed, failed
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// TODO: don't limit to 100 jobs...
	bacjobs, err := runner.Client.List(timeoutCtx, "", []model.IncludedTag{model.IncludedTag(LilypadJobAnnotation)}, nil, 100, false, "created_at", true)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Send()
		return completed, failed
	}

	for _, j := range jobs {
		ctx := log.Ctx(ctx).With().Stringer("id", j.OrderId()).Str("job", j.JobID()).Logger().WithContext(ctx)
		found := false

		for _, bacjob := range bacjobs {
			if bacjob.Job.Metadata.ID != j.JobID() {
				continue
			}

			found = true
			jobStillRunning := job.WaitForTerminalStates()
			jobHasErrors := job.WaitExecutionsThrowErrors([]model.ExecutionStateType{model.ExecutionStateFailed})
			jobComplete := job.WaitForSuccessfulCompletion()

			if ok, err := jobStillRunning(bacjob.State); !ok || err != nil {
				log.Ctx(ctx).Debug().Err(err).Msg("Bacalhau job still in progress")
			} else if ok, err := jobComplete(bacjob.State); ok && err == nil {
				found, cid, stdout, stderr, exitcode := getResult(ctx, bacjob.State.Shards, model.ShardStateCompleted)
				if found {
					log.Ctx(ctx).Info().Err(err).Msg("Bacalhau job completed")
					completed = append(completed, j.Completed(cid, stdout, stderr, exitcode))
				} else {
					log.Ctx(ctx).Error().Msg("No reuslts found for completed job")
					failed = append(failed, j.JobError("No results found for completed job"))
				}
			} else if ok, err := jobHasErrors(bacjob.State); !ok || err != nil {
				found, _, _, stderr, _ := getResult(ctx, bacjob.State.Shards, model.ShardStateCompleted)
				if !found {
					stderr = "Bacalhau job failed"
				}

				log.Ctx(ctx).Info().Err(err).Msg("Bacalhau job failed")
				failed = append(failed, j.JobError(stderr))
			} else {
				// This would be a programming error – we haven't taken account
				// of the states properly.
				log.Ctx(ctx).Warn().Msg("Bacalhau job in unknown state")
			}

			break
		}

		// The job was not seen on the network. This is bad! It may have run but
		// we just can't be sure. So we will have to treat it as failed. It will
		// be retried and someone else may run it again. At least this way the
		// user gets a result – if we just errored out here and refunded the
		// user, someone may still have done some work and we still wouldn't be
		// paying them...
		if !found {
			log.Ctx(ctx).Error().Msg("Bacalhau job not found")
			failed = append(failed, j.JobError("Bacalhau job not found"))
		}
	}

	return completed, failed
}

func getResult(
	ctx context.Context,
	shards map[int]model.ShardState,
	state model.ShardStateType,
) (found bool, result cid.Cid, stdout, stderr string, exitcode int) {
	for _, shard := range maps.Values(shards) {
		for _, execution := range shard.Executions {
			if shard.State == state {
				output := execution.RunOutput
				storage := &execution.PublishedResult

				var err error
				result, err = cid.Parse(storage.CID)
				if err != nil {
					log.Ctx(ctx).Error().Str("cid", storage.CID).Err(err).Msg("Unable to parse result CID")
					continue
				}

				if output != nil {
					stdout = output.STDOUT
					stderr = output.STDERR
					exitcode = output.ExitCode
				}

				return true, result, stdout, stderr, exitcode
			}
		}
	}

	return
}

var _ JobRunner = (*bacalhauRunner)(nil)

// Returns a real job runner that will make real requests against the Bacalhau network.
func NewJobRunner() JobRunner {
	apiPort := 1234
	apiHost := os.Getenv("BACALHAU_API_HOST")
	if apiHost == "" {
		apiHost = "35.245.115.191"
	}
	client := publicapi.NewRequesterAPIClient(fmt.Sprintf("http://%s:%d", apiHost, apiPort))
	return &bacalhauRunner{Client: client}
}

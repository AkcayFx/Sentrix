package agent

import (
	"context"
	"sync"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// Queue is a simple async task queue that processes flow executions.
type Queue struct {
	orchestrator *Orchestrator
	jobs         chan uuid.UUID
	workers      int
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc

	// cancels stores per-flow cancel functions so we can stop individual flows.
	mu      sync.Mutex
	cancels map[uuid.UUID]context.CancelFunc
}

// NewQueue creates a queue with the given number of workers.
func NewQueue(orchestrator *Orchestrator, workers int) *Queue {
	if workers <= 0 {
		workers = 2
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &Queue{
		orchestrator: orchestrator,
		jobs:         make(chan uuid.UUID, 100),
		workers:      workers,
		ctx:          ctx,
		cancel:       cancel,
		cancels:      make(map[uuid.UUID]context.CancelFunc),
	}

	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	log.Infof("agent queue started with %d workers", workers)
	return q
}

// Enqueue adds a flow ID to the processing queue.
func (q *Queue) Enqueue(flowID uuid.UUID) {
	select {
	case q.jobs <- flowID:
		log.WithField("flow_id", flowID.String()).Info("flow enqueued for execution")
	default:
		log.WithField("flow_id", flowID.String()).Warn("queue full, flow rejected")
	}
}

// StopFlow cancels execution of a specific flow.
func (q *Queue) StopFlow(flowID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if cancel, ok := q.cancels[flowID]; ok {
		cancel()
		delete(q.cancels, flowID)
		log.WithField("flow_id", flowID.String()).Info("flow execution cancelled")
		return true
	}
	return false
}

// Shutdown gracefully stops the queue and waits for workers to finish.
func (q *Queue) Shutdown() {
	log.Info("shutting down agent queue...")
	q.cancel()
	close(q.jobs)
	q.wg.Wait()
	log.Info("agent queue shutdown complete")
}

func (q *Queue) worker(id int) {
	defer q.wg.Done()

	for flowID := range q.jobs {
		select {
		case <-q.ctx.Done():
			return
		default:
		}

		// Create a per-flow context that can be cancelled individually.
		flowCtx, flowCancel := context.WithCancel(q.ctx)

		q.mu.Lock()
		q.cancels[flowID] = flowCancel
		q.mu.Unlock()

		log.WithFields(log.Fields{
			"worker":  id,
			"flow_id": flowID.String(),
		}).Info("worker: starting flow execution")

		err := q.orchestrator.ExecuteFlow(flowCtx, flowID)
		if err != nil {
			log.WithFields(log.Fields{
				"worker":  id,
				"flow_id": flowID.String(),
				"error":   err.Error(),
			}).Error("worker: flow execution failed")
		}

		// Cleanup.
		flowCancel()
		q.mu.Lock()
		delete(q.cancels, flowID)
		q.mu.Unlock()
	}
}

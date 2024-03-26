package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/nyaruka/gocommon/dates"
	"github.com/nyaruka/gocommon/jsonx"
	"github.com/nyaruka/mailroom/core/models"
	"github.com/nyaruka/mailroom/core/tasks"
	"github.com/nyaruka/mailroom/runtime"
	"github.com/nyaruka/mailroom/utils/queues"
	"github.com/pkg/errors"
)

// Task is the interface for all handler tasks - tasks which operate on a single contact
type Task interface {
	Type() string
	Perform(ctx context.Context, rt *runtime.Runtime, oa *models.OrgAssets, contactID models.ContactID) error
}

var registeredTypes = map[string]func() Task{}

func RegisterTaskType(name string, initFunc func() Task) {
	registeredTypes[name] = initFunc
}

func readTask(type_ string, data []byte) (Task, error) {
	fn := registeredTypes[type_]
	if fn == nil {
		return nil, errors.Errorf("unknown task type: %s", type_)
	}

	t := fn()
	return t, json.Unmarshal(data, t)
}

// wrapper for encoding a handler task
type payload struct {
	Type       string          `json:"type"`
	Task       json.RawMessage `json:"task"`
	QueuedOn   time.Time       `json:"queued_on"`
	ErrorCount int             `json:"error_count,omitempty"`
}

// QueueTask queues a handler task for the given contact
func QueueTask(rc redis.Conn, orgID models.OrgID, contactID models.ContactID, task Task) error {
	return queueTask(rc, orgID, contactID, task, false, 0)
}

func queueTask(rc redis.Conn, orgID models.OrgID, contactID models.ContactID, task Task, front bool, errorCount int) error {
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return errors.Wrapf(err, "error marshalling handler task")
	}

	payload := &payload{Type: task.Type(), Task: taskJSON, QueuedOn: dates.Now(), ErrorCount: errorCount}
	payloadJSON := jsonx.MustMarshal(payload)

	// first push the event on our contact queue
	contactQ := fmt.Sprintf("c:%d:%d", orgID, contactID)
	if front {
		_, err = redis.Int64(rc.Do("LPUSH", contactQ, string(payloadJSON)))

	} else {
		_, err = redis.Int64(rc.Do("RPUSH", contactQ, string(payloadJSON)))
	}
	if err != nil {
		return errors.Wrapf(err, "error queuing handler task")
	}

	// then add a handle task for that contact on our global handler queue to
	err = tasks.Queue(rc, tasks.HandlerQueue, orgID, &HandleContactEventTask{ContactID: contactID}, queues.DefaultPriority)
	if err != nil {
		return errors.Wrapf(err, "error queuing handle task")
	}
	return nil
}

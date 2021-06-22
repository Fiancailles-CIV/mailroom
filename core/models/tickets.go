package models

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"
	"time"

	"github.com/nyaruka/gocommon/dates"
	"github.com/nyaruka/gocommon/httpx"
	"github.com/nyaruka/goflow/assets"
	"github.com/nyaruka/goflow/flows"
	"github.com/nyaruka/goflow/utils"
	"github.com/nyaruka/mailroom/core/goflow"
	"github.com/nyaruka/mailroom/utils/dbutil"
	"github.com/nyaruka/null"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type TicketID null.Int

// NilTicketID is our constant for a nil ticket id
const NilTicketID = TicketID(0)

// MarshalJSON marshals into JSON. 0 values will become null
func (i TicketID) MarshalJSON() ([]byte, error) {
	return null.Int(i).MarshalJSON()
}

// UnmarshalJSON unmarshals from JSON. null values become 0
func (i *TicketID) UnmarshalJSON(b []byte) error {
	return null.UnmarshalInt(b, (*null.Int)(i))
}

// Value returns the db value, null is returned for 0
func (i TicketID) Value() (driver.Value, error) {
	return null.Int(i).Value()
}

// Scan scans from the db value. null values become 0
func (i *TicketID) Scan(value interface{}) error {
	return null.ScanInt(value, (*null.Int)(i))
}

type TicketerID null.Int
type TicketStatus string

const (
	TicketStatusOpen   = TicketStatus("O")
	TicketStatusClosed = TicketStatus("C")
)

// Register a ticket service factory with the engine
func init() {
	goflow.RegisterTicketServiceFactory(
		func(session flows.Session, ticketer *flows.Ticketer) (flows.TicketService, error) {
			return ticketer.Asset().(*Ticketer).AsService(ticketer)
		},
	)
}

type Ticket struct {
	t struct {
		ID             TicketID         `db:"id"`
		UUID           flows.TicketUUID `db:"uuid"`
		OrgID          OrgID            `db:"org_id"`
		ContactID      ContactID        `db:"contact_id"`
		TicketerID     TicketerID       `db:"ticketer_id"`
		ExternalID     null.String      `db:"external_id"`
		Status         TicketStatus     `db:"status"`
		Subject        string           `db:"subject"`
		Body           string           `db:"body"`
		Config         null.Map         `db:"config"`
		OpenedOn       time.Time        `db:"opened_on"`
		ModifiedOn     time.Time        `db:"modified_on"`
		ClosedOn       *time.Time       `db:"closed_on"`
		LastActivityOn time.Time        `db:"last_activity_on"`
	}
}

// NewTicket creates a new open ticket
func NewTicket(uuid flows.TicketUUID, orgID OrgID, contactID ContactID, ticketerID TicketerID, externalID, subject, body string, config map[string]interface{}) *Ticket {
	t := &Ticket{}
	t.t.UUID = uuid
	t.t.OrgID = orgID
	t.t.ContactID = contactID
	t.t.TicketerID = ticketerID
	t.t.ExternalID = null.String(externalID)
	t.t.Status = TicketStatusOpen
	t.t.Subject = subject
	t.t.Body = body
	t.t.Config = null.NewMap(config)
	return t
}

func (t *Ticket) ID() TicketID              { return t.t.ID }
func (t *Ticket) UUID() flows.TicketUUID    { return t.t.UUID }
func (t *Ticket) OrgID() OrgID              { return t.t.OrgID }
func (t *Ticket) ContactID() ContactID      { return t.t.ContactID }
func (t *Ticket) TicketerID() TicketerID    { return t.t.TicketerID }
func (t *Ticket) ExternalID() null.String   { return t.t.ExternalID }
func (t *Ticket) Status() TicketStatus      { return t.t.Status }
func (t *Ticket) Subject() string           { return t.t.Subject }
func (t *Ticket) Body() string              { return t.t.Body }
func (t *Ticket) LastActivityOn() time.Time { return t.t.LastActivityOn }
func (t *Ticket) Config(key string) string {
	return t.t.Config.GetString(key, "")
}

func (t *Ticket) FlowTicket(oa *OrgAssets) (*flows.Ticket, error) {
	modelTicketer := oa.TicketerByID(t.TicketerID())
	if modelTicketer == nil {
		return nil, errors.New("unable to load ticketer with id %d")
	}

	return flows.NewTicket(
		t.UUID(),
		oa.SessionAssets().Ticketers().Get(modelTicketer.UUID()),
		t.Subject(),
		t.Body(),
		string(t.ExternalID()),
	), nil
}

// ForwardIncoming forwards an incoming message from a contact to this ticket
func (t *Ticket) ForwardIncoming(ctx context.Context, db Queryer, org *OrgAssets, msgUUID flows.MsgUUID, text string, attachments []utils.Attachment) error {
	ticketer := org.TicketerByID(t.t.TicketerID)
	if ticketer == nil {
		return errors.Errorf("can't find ticketer with id %d", t.t.TicketerID)
	}

	service, err := ticketer.AsService(flows.NewTicketer(ticketer))
	if err != nil {
		return err
	}

	logger := &HTTPLogger{}
	err = service.Forward(t, msgUUID, text, attachments, logger.Ticketer(ticketer))

	logger.Insert(ctx, db)

	return err
}

const selectOpenTicketsSQL = `
SELECT
  t.id AS id,
  t.uuid AS uuid,
  t.org_id AS org_id,
  t.contact_id AS contact_id,
  t.ticketer_id AS ticketer_id,
  t.external_id AS external_id,
  t.status AS status,
  t.subject AS subject,
  t.body AS body,
  t.config AS config,
  t.opened_on AS opened_on,
  t.modified_on AS modified_on,
  t.closed_on AS closed_on,
  t.last_activity_on AS last_activity_on
FROM
  tickets_ticket t
WHERE
  t.contact_id = $1 AND
  t.status = 'O'
`

// LoadOpenTicketsForContact looks up the open tickets for the passed in contact
func LoadOpenTicketsForContact(ctx context.Context, db Queryer, contact *Contact) ([]*Ticket, error) {
	return loadTickets(ctx, db, selectOpenTicketsSQL, contact.ID())
}

const selectTicketsByIDSQL = `
SELECT
  t.id AS id,
  t.uuid AS uuid,
  t.org_id AS org_id,
  t.contact_id AS contact_id,
  t.ticketer_id AS ticketer_id,
  t.external_id AS external_id,
  t.status AS status,
  t.subject AS subject,
  t.body AS body,
  t.config AS config,
  t.opened_on AS opened_on,
  t.modified_on AS modified_on,
  t.closed_on AS closed_on,
  t.last_activity_on AS last_activity_on
FROM
  tickets_ticket t
WHERE
  t.id = ANY($1)
`

// LoadTickets loads all of the tickets with the given ids
func LoadTickets(ctx context.Context, db Queryer, ids []TicketID) ([]*Ticket, error) {
	return loadTickets(ctx, db, selectTicketsByIDSQL, pq.Array(ids))
}

func loadTickets(ctx context.Context, db Queryer, query string, params ...interface{}) ([]*Ticket, error) {
	rows, err := db.QueryxContext(ctx, query, params...)
	if err != nil && err != sql.ErrNoRows {
		return nil, errors.Wrap(err, "error loading tickets")
	}
	defer rows.Close()

	tickets := make([]*Ticket, 0, 2)
	for rows.Next() {
		ticket := &Ticket{}
		err = rows.StructScan(&ticket.t)
		if err != nil {
			return nil, errors.Wrapf(err, "error unmarshalling ticket")
		}
		tickets = append(tickets, ticket)
	}

	return tickets, nil
}

const selectTicketByUUIDSQL = `
SELECT
  id,
  uuid,
  org_id,
  contact_id,
  ticketer_id,
  external_id,
  status,
  subject,
  body,
  config,
  opened_on,
  modified_on,
  closed_on,
  last_activity_on
FROM
  tickets_ticket
WHERE
  uuid = $1
`

// LookupTicketByUUID looks up the ticket with the passed in UUID
func LookupTicketByUUID(ctx context.Context, db *sqlx.DB, uuid flows.TicketUUID) (*Ticket, error) {
	return lookupTicket(ctx, db, selectTicketByUUIDSQL, uuid)
}

const selectTicketByExternalIDSQL = `
SELECT
  id,
  uuid,
  org_id,
  contact_id,
  ticketer_id,
  external_id,
  status,
  subject,
  body,
  config,
  opened_on,
  modified_on,
  closed_on,
  last_activity_on
FROM
  tickets_ticket
WHERE
  ticketer_id = $1 AND
  external_id = $2
`

// LookupTicketByExternalID looks up the ticket with the passed in ticketer and external ID
func LookupTicketByExternalID(ctx context.Context, db Queryer, ticketerID TicketerID, externalID string) (*Ticket, error) {
	return lookupTicket(ctx, db, selectTicketByExternalIDSQL, ticketerID, externalID)
}

func lookupTicket(ctx context.Context, db Queryer, query string, params ...interface{}) (*Ticket, error) {
	rows, err := db.QueryxContext(ctx, query, params...)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	defer rows.Close()

	if err == sql.ErrNoRows || !rows.Next() {
		return nil, nil
	}

	ticket := &Ticket{}
	err = rows.StructScan(&ticket.t)
	if err != nil {
		return nil, err
	}

	return ticket, nil
}

const insertTicketSQL = `
INSERT INTO 
  tickets_ticket(uuid,  org_id,  contact_id,  ticketer_id,  external_id,  status,  subject,  body,  config,  opened_on, modified_on, last_activity_on)
  VALUES(        :uuid, :org_id, :contact_id, :ticketer_id, :external_id, :status, :subject, :body, :config, NOW(),     NOW()      , NOW())
RETURNING
  id
`

// InsertTickets inserts the passed in tickets returning any errors encountered
func InsertTickets(ctx context.Context, tx Queryer, tickets []*Ticket) error {
	if len(tickets) == 0 {
		return nil
	}

	ts := make([]interface{}, len(tickets))
	for i := range tickets {
		ts[i] = &tickets[i].t
	}

	return BulkQuery(ctx, "inserted tickets", tx, insertTicketSQL, ts)
}

// UpdateTicketExternalID updates the external ID of the given ticket
func UpdateTicketExternalID(ctx context.Context, db Queryer, ticket *Ticket, externalID string) error {
	t := &ticket.t
	t.ExternalID = null.String(externalID)
	return Exec(ctx, "update ticket external ID", db, `UPDATE tickets_ticket SET external_id = $2 WHERE id = $1`, t.ID, t.ExternalID)
}

// UpdateTicketConfig updates the passed in ticket's config with any passed in values
func UpdateTicketConfig(ctx context.Context, db Queryer, ticket *Ticket, config map[string]string) error {
	t := &ticket.t
	for key, value := range config {
		t.Config.Map()[key] = value
	}

	return Exec(ctx, "update ticket config", db, `UPDATE tickets_ticket SET config = $2 WHERE id = $1`, t.ID, t.Config)
}

// UpdateTicketLastActivity updates the last_activity_on of the given tickets to be now
func UpdateTicketLastActivity(ctx context.Context, db Queryer, tickets []*Ticket) error {
	now := dates.Now()
	ids := make([]TicketID, len(tickets))
	for i, t := range tickets {
		t.t.LastActivityOn = now
		ids[i] = t.ID()
	}
	return updateTicketLastActivity(ctx, db, ids, now)
}

func updateTicketLastActivity(ctx context.Context, db Queryer, ids []TicketID, now time.Time) error {
	return Exec(ctx, "update ticket last activity", db, `UPDATE tickets_ticket SET last_activity_on = $2 WHERE id = ANY($1)`, pq.Array(ids), now)
}

const closeTicketSQL = `
UPDATE
  tickets_ticket
SET
  status = 'C',
  modified_on = $2,
  closed_on = $2,
  last_activity_on = $2
WHERE
  id = ANY($1)
`

// CloseTickets closes the passed in tickets
func CloseTickets(ctx context.Context, db Queryer, oa *OrgAssets, userID UserID, tickets []*Ticket, externally bool, logger *HTTPLogger) (map[*Ticket]*TicketEvent, error) {
	byTicketer := make(map[TicketerID][]*Ticket)
	ids := make([]TicketID, 0, len(tickets))
	events := make([]*TicketEvent, 0, len(tickets))
	eventsByTicket := make(map[*Ticket]*TicketEvent, len(tickets))
	now := dates.Now()

	for _, ticket := range tickets {
		if ticket.Status() != TicketStatusClosed {
			byTicketer[ticket.TicketerID()] = append(byTicketer[ticket.TicketerID()], ticket)
			ids = append(ids, ticket.ID())
			t := &ticket.t
			t.Status = TicketStatusClosed
			t.ModifiedOn = now
			t.ClosedOn = &now
			t.LastActivityOn = now

			e := NewTicketEvent(ticket.OrgID(), userID, ticket.ContactID(), ticket.ID(), TicketEventTypeClosed)
			events = append(events, e)
			eventsByTicket[ticket] = e
		}
	}

	if externally {
		for ticketerID, ticketerTickets := range byTicketer {
			ticketer := oa.TicketerByID(ticketerID)
			if ticketer != nil {
				service, err := ticketer.AsService(flows.NewTicketer(ticketer))
				if err != nil {
					return nil, err
				}

				err = service.Close(ticketerTickets, logger.Ticketer(ticketer))
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// mark the tickets as closed in the db
	err := Exec(ctx, "close tickets", db, closeTicketSQL, pq.Array(ids), now)
	if err != nil {
		return nil, errors.Wrapf(err, "error updating tickets")
	}

	err = InsertTicketEvents(ctx, db, events)
	if err != nil {
		return nil, errors.Wrapf(err, "error inserting ticket events")
	}

	return eventsByTicket, nil
}

const reopenTicketSQL = `
UPDATE
  tickets_ticket
SET
  status = 'O',
  modified_on = $2,
  closed_on = NULL,
  last_activity_on = $2
WHERE
  id = ANY($1)
`

// ReopenTickets reopens the passed in tickets
func ReopenTickets(ctx context.Context, db Queryer, org *OrgAssets, userID UserID, tickets []*Ticket, externally bool, logger *HTTPLogger) (map[*Ticket]*TicketEvent, error) {
	byTicketer := make(map[TicketerID][]*Ticket)
	ids := make([]TicketID, 0, len(tickets))
	events := make([]*TicketEvent, 0, len(tickets))
	eventsByTicket := make(map[*Ticket]*TicketEvent, len(tickets))
	now := dates.Now()

	for _, ticket := range tickets {
		if ticket.Status() != TicketStatusOpen {
			byTicketer[ticket.TicketerID()] = append(byTicketer[ticket.TicketerID()], ticket)
			ids = append(ids, ticket.ID())
			t := &ticket.t
			t.Status = TicketStatusOpen
			t.ModifiedOn = now
			t.ClosedOn = nil
			t.LastActivityOn = now

			e := NewTicketEvent(ticket.OrgID(), userID, ticket.ContactID(), ticket.ID(), TicketEventTypeReopened)
			events = append(events, e)
			eventsByTicket[ticket] = e
		}
	}

	if externally {
		for ticketerID, ticketerTickets := range byTicketer {
			ticketer := org.TicketerByID(ticketerID)
			if ticketer != nil {
				service, err := ticketer.AsService(flows.NewTicketer(ticketer))
				if err != nil {
					return nil, err
				}

				err = service.Reopen(ticketerTickets, logger.Ticketer(ticketer))
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// mark the tickets as opened in the db
	err := Exec(ctx, "reopen tickets", db, reopenTicketSQL, pq.Array(ids), now)
	if err != nil {
		return nil, errors.Wrapf(err, "error updating tickets")
	}

	err = InsertTicketEvents(ctx, db, events)
	if err != nil {
		return nil, errors.Wrapf(err, "error inserting ticket events")
	}

	return eventsByTicket, nil
}

// Ticketer is our type for a ticketer asset
type Ticketer struct {
	t struct {
		ID     TicketerID          `json:"id"`
		UUID   assets.TicketerUUID `json:"uuid"`
		OrgID  OrgID               `json:"org_id"`
		Type   string              `json:"ticketer_type"`
		Name   string              `json:"name"`
		Config map[string]string   `json:"config"`
	}
}

// ID returns the ID
func (t *Ticketer) ID() TicketerID { return t.t.ID }

// UUID returns the UUID
func (t *Ticketer) UUID() assets.TicketerUUID { return t.t.UUID }

// OrgID returns the org ID
func (t *Ticketer) OrgID() OrgID { return t.t.OrgID }

// Name returns the name
func (t *Ticketer) Name() string { return t.t.Name }

// Type returns the type
func (t *Ticketer) Type() string { return t.t.Type }

// Config returns the named config value
func (t *Ticketer) Config(key string) string { return t.t.Config[key] }

// Reference returns an asset reference to this ticketer
func (t *Ticketer) Reference() *assets.TicketerReference {
	return assets.NewTicketerReference(t.t.UUID, t.t.Name)
}

// AsService builds the corresponding engine service for the passed in Ticketer
func (t *Ticketer) AsService(ticketer *flows.Ticketer) (TicketService, error) {
	httpClient, httpRetries, _ := goflow.HTTP()

	initFunc := ticketServices[t.Type()]
	if initFunc != nil {
		return initFunc(httpClient, httpRetries, ticketer, t.t.Config)
	}

	return nil, errors.Errorf("unrecognized ticket service type '%s'", t.Type())
}

// UpdateConfig updates the configuration of this ticketer with the given values
func (t *Ticketer) UpdateConfig(ctx context.Context, db Queryer, add map[string]string, remove map[string]bool) error {
	for key, value := range add {
		t.t.Config[key] = value
	}
	for key := range remove {
		delete(t.t.Config, key)
	}

	// convert to null.Map to save
	dbMap := make(map[string]interface{}, len(t.t.Config))
	for key, value := range t.t.Config {
		dbMap[key] = value
	}

	return Exec(ctx, "update ticketer config", db, `UPDATE tickets_ticketer SET config = $2 WHERE id = $1`, t.t.ID, null.NewMap(dbMap))
}

// TicketService extends the engine's ticket service and adds support for forwarding new incoming messages
type TicketService interface {
	flows.TicketService

	Forward(*Ticket, flows.MsgUUID, string, []utils.Attachment, flows.HTTPLogCallback) error
	Close([]*Ticket, flows.HTTPLogCallback) error
	Reopen([]*Ticket, flows.HTTPLogCallback) error
}

// TicketServiceFunc is a func which creates a ticket service
type TicketServiceFunc func(httpClient *http.Client, httpRetries *httpx.RetryConfig, ticketer *flows.Ticketer, config map[string]string) (TicketService, error)

var ticketServices = map[string]TicketServiceFunc{}

// RegisterTicketService registers a new ticket service
func RegisterTicketService(name string, initFunc TicketServiceFunc) {
	ticketServices[name] = initFunc
}

const selectTicketerByUUIDSQL = `
SELECT ROW_TO_JSON(r) FROM (SELECT
	t.id as id,
	t.uuid as uuid,
	t.org_id as org_id,
	t.name as name,
	t.ticketer_type as ticketer_type,
	t.config as config
FROM 
	tickets_ticketer t
WHERE 
	t.uuid = $1 AND 
	t.is_active = TRUE
) r;
`

// LookupTicketerByUUID looks up the ticketer with the passed in UUID
func LookupTicketerByUUID(ctx context.Context, db Queryer, uuid assets.TicketerUUID) (*Ticketer, error) {
	rows, err := db.QueryxContext(ctx, selectTicketerByUUIDSQL, string(uuid))
	if err != nil && err != sql.ErrNoRows {
		return nil, errors.Wrapf(err, "error querying for ticketer for uuid: %s", string(uuid))
	}
	defer rows.Close()

	if err == sql.ErrNoRows || !rows.Next() {
		return nil, nil
	}

	ticketer := &Ticketer{}
	err = dbutil.ReadJSONRow(rows, &ticketer.t)
	if err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling ticketer")
	}

	return ticketer, nil
}

const selectOrgTicketersSQL = `
SELECT ROW_TO_JSON(r) FROM (SELECT
	t.id as id,
	t.uuid as uuid,
	t.org_id as org_id,
	t.name as name,
	t.ticketer_type as ticketer_type,
	t.config as config
FROM
	tickets_ticketer t
WHERE
	t.org_id = $1 AND
	t.is_active = TRUE
ORDER BY
	t.created_on ASC
) r;
`

// loadTicketers loads all the ticketers for the passed in org
func loadTicketers(ctx context.Context, db sqlx.Queryer, orgID OrgID) ([]assets.Ticketer, error) {
	start := time.Now()

	rows, err := db.Queryx(selectOrgTicketersSQL, orgID)
	if err != nil && err != sql.ErrNoRows {
		return nil, errors.Wrapf(err, "error querying ticketers for org: %d", orgID)
	}
	defer rows.Close()

	ticketers := make([]assets.Ticketer, 0, 2)
	for rows.Next() {
		ticketer := &Ticketer{}
		err := dbutil.ReadJSONRow(rows, &ticketer.t)
		if err != nil {
			return nil, errors.Wrapf(err, "error unmarshalling ticketer")
		}
		ticketers = append(ticketers, ticketer)
	}

	logrus.WithField("elapsed", time.Since(start)).WithField("org_id", orgID).WithField("count", len(ticketers)).Debug("loaded ticketers")

	return ticketers, nil
}

// MarshalJSON marshals into JSON. 0 values will become null
func (i TicketerID) MarshalJSON() ([]byte, error) {
	return null.Int(i).MarshalJSON()
}

// UnmarshalJSON unmarshals from JSON. null values become 0
func (i *TicketerID) UnmarshalJSON(b []byte) error {
	return null.UnmarshalInt(b, (*null.Int)(i))
}

// Value returns the db value, null is returned for 0
func (i TicketerID) Value() (driver.Value, error) {
	return null.Int(i).Value()
}

// Scan scans from the db value. null values become 0
func (i *TicketerID) Scan(value interface{}) error {
	return null.ScanInt(value, (*null.Int)(i))
}

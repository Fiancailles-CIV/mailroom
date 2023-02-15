package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/nyaruka/mailroom/core/models"
	"github.com/nyaruka/mailroom/runtime"
	"github.com/pkg/errors"
)

type JSONHandler func(ctx context.Context, rt *runtime.Runtime, r *http.Request) (any, int, error)

func JSONRequestResponse(handler JSONHandler) Handler {
	return func(ctx context.Context, rt *runtime.Runtime, r *http.Request, w http.ResponseWriter) error {
		w.Header().Set("Content-type", "application/json")

		value, status, err := handler(ctx, rt, r)
		if err != nil {
			return err
		}

		// handler returned an error to use as the response
		asError, isError := value.(error)
		if isError {
			value = NewErrorResponse(asError)
		}

		return WriteMarshalled(w, status, value)
	}
}

// RequireUserToken wraps a handler to require passing of an API token via the authorization header
func RequireUserToken(handler Handler) Handler {
	return func(ctx context.Context, rt *runtime.Runtime, r *http.Request, w http.ResponseWriter) error {
		token := r.Header.Get("authorization")

		if !strings.HasPrefix(token, "Token ") {
			return WriteMarshalled(w, http.StatusUnauthorized, NewErrorResponse(errors.New("missing authorization token")))
		}

		// pull out the actual token
		token = token[6:]

		// try to look it up
		rows, err := rt.DB.QueryContext(ctx, `
		SELECT 
			user_id, 
			org_id
		FROM
			api_apitoken t
			JOIN orgs_org o ON t.org_id = o.id
			JOIN auth_group g ON t.role_id = g.id
			JOIN auth_user u ON t.user_id = u.id
		WHERE
			key = $1 AND
			g.name IN ('Administrators', 'Editors', 'Surveyors') AND
			t.is_active = TRUE AND
			o.is_active = TRUE AND
			u.is_active = TRUE
		`, token)
		if err != nil {
			return errors.Wrap(err, "error querying API token")
		}

		defer rows.Close()

		if !rows.Next() {
			return WriteMarshalled(w, http.StatusUnauthorized, NewErrorResponse(errors.New("invalid authorization token")))
		}

		var userID int64
		var orgID models.OrgID
		err = rows.Scan(&userID, &orgID)
		if err != nil {
			return errors.Wrap(err, "error scanning auth row")
		}

		// we are authenticated set our user id ang org id on our context and continue
		ctx = context.WithValue(ctx, UserIDKey, userID)
		ctx = context.WithValue(ctx, OrgIDKey, orgID)

		return handler(ctx, rt, r, w)
	}
}

// RequireAuthToken wraps a handler to require that our request to have our global authorization header
func RequireAuthToken(handler Handler) Handler {
	return func(ctx context.Context, rt *runtime.Runtime, r *http.Request, w http.ResponseWriter) error {
		auth := r.Header.Get("authorization")

		if rt.Config.AuthToken != "" && fmt.Sprintf("Token %s", rt.Config.AuthToken) != auth {
			return WriteMarshalled(w, http.StatusUnauthorized, NewErrorResponse(errors.New("invalid or missing authorization header")))
		}

		// we are authenticated, call our chain
		return handler(ctx, rt, r, w)
	}
}

// LoggingJSONHandler is a JSON web handler which logs HTTP logs
type LoggingJSONHandler func(ctx context.Context, rt *runtime.Runtime, r *http.Request, l *models.HTTPLogger) (any, int, error)

// WithHTTPLogs wraps a handler to create a handler which can record and save HTTP logs
func WithHTTPLogs(handler LoggingJSONHandler) JSONHandler {
	return func(ctx context.Context, rt *runtime.Runtime, r *http.Request) (any, int, error) {
		logger := &models.HTTPLogger{}

		response, status, err := handler(ctx, rt, r, logger)

		if err := logger.Insert(ctx, rt.DB); err != nil {
			return nil, 0, errors.Wrap(err, "error writing HTTP logs")
		}

		return response, status, err
	}
}

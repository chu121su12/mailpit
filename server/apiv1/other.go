package apiv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/htmlcheck"
	"github.com/axllent/mailpit/internal/linkcheck"
	"github.com/axllent/mailpit/internal/spamassassin"
	"github.com/axllent/mailpit/internal/storage"
	"github.com/axllent/mailpit/internal/tools"
	"github.com/gorilla/mux"
)

// HTMLCheck returns a summary of the HTML client support
func HTMLCheck(w http.ResponseWriter, r *http.Request) {
	// swagger:route GET /api/v1/message/{ID}/html-check other HTMLCheckParams
	//
	// # HTML check
	//
	// Returns the summary of the message HTML checker.
	//
	// The ID can be set to `latest` to return the latest message.
	//
	//	Produces:
	//	  - application/json
	//
	//	Schemes: http, https
	//
	//	Responses:
	//	  200: HTMLCheckResponse
	//    400: ErrorResponse
	//    404: NotFoundResponse

	vars := mux.Vars(r)
	id := vars["id"]

	if id == "latest" {
		var err error
		id, err = storage.LatestID(r)
		if err != nil {
			fourOFour(w)
			return
		}
	}

	raw, err := storage.GetMessageRaw(id)
	if err != nil {
		fourOFour(w)
		return
	}

	e := bytes.NewReader(raw)

	msg, _, err := storage.GetMailHeader(storage.GetMailboxes(r), id, e)
	if err != nil {
		tools.BasicAuthResponse(w)
		return
	}

	_, err = mail.ReadMessage(e)
	if err != nil {
		httpError(w, err.Error())
		return
	}

	if msg.HTML == "" {
		httpError(w, "message does not contain HTML")
		return
	}

	checks, err := htmlcheck.RunTests(msg.HTML)
	if err != nil {
		httpError(w, err.Error())
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(checks); err != nil {
		httpError(w, err.Error())
	}
}

// LinkCheck returns a summary of links in the email
func LinkCheck(w http.ResponseWriter, r *http.Request) {
	// swagger:route GET /api/v1/message/{ID}/link-check other LinkCheckParams
	//
	// # Link check
	//
	// Returns the summary of the message Link checker.
	//
	// The ID can be set to `latest` to return the latest message.
	//
	//	Produces:
	//	  - application/json
	//
	//	Schemes: http, https
	//
	//	Responses:
	//	  200: LinkCheckResponse
	//    400: ErrorResponse
	//    404: NotFoundResponse

	if config.DemoMode {
		httpError(w, "this functionality has been disabled for demonstration purposes")
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	if id == "latest" {
		var err error
		id, err = storage.LatestID(r)
		if err != nil {
			fourOFour(w)
			return
		}
	}

	msg, err := storage.GetMessage(storage.GetMailboxes(r), id)
	if err != nil {
		fourOFour(w)
		return
	}

	f := r.URL.Query().Get("follow")
	followRedirects := f == "true" || f == "1"

	summary, err := linkcheck.RunTests(msg, followRedirects)
	if err != nil {
		httpError(w, err.Error())
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		httpError(w, err.Error())
	}
}

// SpamAssassinCheck returns a summary of SpamAssassin results (if enabled)
func SpamAssassinCheck(w http.ResponseWriter, r *http.Request) {
	// swagger:route GET /api/v1/message/{ID}/sa-check other SpamAssassinCheckParams
	//
	// # SpamAssassin check
	//
	// Returns the SpamAssassin summary (if enabled) of the message.
	//
	// The ID can be set to `latest` to return the latest message.
	//
	//	Produces:
	//	  - application/json
	//
	//	Schemes: http, https
	//
	//	Responses:
	//	  200: SpamAssassinResponse
	//    400: ErrorResponse
	//    404: NotFoundResponse

	vars := mux.Vars(r)
	id := vars["id"]

	if id == "latest" {
		var err error
		id, err = storage.LatestID(r)
		if err != nil {
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, err.Error())
			return
		}
	}

	msg, err := storage.GetMessageRaw(id)
	if err != nil {
		fourOFour(w)
		return
	}

	_, _, err = storage.GetMailHeader(storage.GetMailboxes(r), id, bytes.NewReader(msg))
	if err != nil {
		tools.BasicAuthResponse(w)
		return
	}

	summary, err := spamassassin.Check(msg)
	if err != nil {
		httpError(w, err.Error())
		return
	}

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		httpError(w, err.Error())
	}
}

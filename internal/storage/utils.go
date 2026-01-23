package storage

import (
	"bytes"
	"errors"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/html2text"
	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/tools"
	"github.com/jhillyerd/enmime/v2"
	"github.com/leporo/sqlf"
)

var (
	// for stats to prevent import cycle
	mu sync.RWMutex
	// StatsDeleted for counting the number of messages deleted
	StatsDeleted uint64
)

func HasMailboxFeature() bool {
	return config.UIUserMailHeader == ""
}

// GetMailboxes resolves mailbox from request header via config.UIUserMailHeader
func GetMailboxes(r *http.Request) []string {
	if config.UIUserMailHeader == "" {
		return []string{}
	}

	var h = r.Header.Get(config.UIUserMailHeader)
	if h == "*" {
		return []string{}
	}
	return []string{h}
}

// GetMailHeader check allowed TO: by parsing mail header and checking metadata from DB
func GetMailHeader(mailbox []string, id string, reader *bytes.Reader) (*enmime.Envelope, *Metadata, error) {
	parser := enmime.NewParser(enmime.DisableCharacterDetection(true))

	env, err := parser.ReadEnvelope(reader)
	if err != nil {
		return nil, nil, err
	}

	meta, err := GetMetadata(mailbox, id)
	if err != nil {
		meta = Metadata{}
	}

	if len(mailbox) == 0 {
		return env, &meta, nil
	}

	tos := meta.To
	if tos == nil {
		toData := addressToSlice(env, "To")
		if len(toData) > 0 {
			tos = toData
		} else if env.GetHeader("To") != "" {
			tos = []*mail.Address{{Name: env.GetHeader("To")}}
		}
	}

	if canSend(mailbox, tos) {
		return env, &meta, nil
	}

	return nil, nil, errors.New("403")
}

// AddTempFile adds a file to the slice of files to delete on exit
func AddTempFile(s string) {
	temporaryFiles = append(temporaryFiles, s)
}

// DeleteTempFiles will delete files added via AddTempFiles
func deleteTempFiles() {
	for _, f := range temporaryFiles {
		if err := os.Remove(f); err == nil {
			logger.Log().Debugf("removed temporary file: %s", f)
		}
	}
}

// Return a header field as a []*mail.Address, or "null" is not found/empty
func addressToSlice(env *enmime.Envelope, key string) []*mail.Address {
	data, err := env.AddressList(key)
	if err != nil || data == nil {
		return []*mail.Address{}
	}

	return data
}

// Generate the search text based on some header fields (to, from, subject etc)
// and either the stripped HTML body (if exists) or text body
func createSearchText(env *enmime.Envelope) string {
	var b strings.Builder

	b.WriteString(env.GetHeader("From") + " ")
	b.WriteString(env.GetHeader("Subject") + " ")
	b.WriteString(env.GetHeader("To") + " ")
	b.WriteString(env.GetHeader("Cc") + " ")
	b.WriteString(env.GetHeader("Bcc") + " ")
	b.WriteString(env.GetHeader("Reply-To") + " ")
	b.WriteString(env.GetHeader("Return-Path") + " ")

	h := html2text.Strip(env.HTML, true)
	if h != "" {
		b.WriteString(h + " ")
	} else {
		b.WriteString(env.Text + " ")
	}
	// add attachment filenames
	for _, a := range env.Attachments {
		b.WriteString(a.FileName + " ")
	}

	d := cleanString(b.String())

	return d
}

// CleanString removes unwanted characters from stored search text and search queries
func cleanString(str string) string {
	// replace \uFEFF with space, see https://github.com/golang/go/issues/42274#issuecomment-1017258184
	str = strings.ReplaceAll(str, string('\uFEFF'), " ")

	// remove/replace new lines
	re := regexp.MustCompile(`(\r?\n|\t|>|<|"|\,|;|\(|\))`)
	str = re.ReplaceAllString(str, " ")

	// remove duplicate whitespace and trim
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(str)), " "))
}

// LogMessagesDeleted logs the number of messages deleted
func logMessagesDeleted(n int) {
	mu.Lock()
	StatsDeleted = StatsDeleted + tools.SafeUint64(n)
	mu.Unlock()
}

// IsFile returns whether a path is a file
func isFile(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) || !info.Mode().IsRegular() {
		return false
	}

	return true
}

// Convert `%` to `%%` for SQL searches
func escPercentChar(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

// mbStmtFilter adds where condition to sql statement to check allowed TO:
func mbStmtFilter(mailbox []string, q *sqlf.Stmt) *sqlf.Stmt {
	if len(mailbox) == 0 {
		return q
	}

	for _, m := range mailbox {
		q = q.Where("IFNULL(json_extract(Metadata, '$.To'), '{}') LIKE ?", "%"+escPercentChar(m)+"%")
	}
	return q
}

// canSend checks address for allowed TO:
func canSend(mailbox []string, tos []*mail.Address) bool {
	if len(mailbox) == 0 {
		return true
	}

	for _, a := range tos {
		for _, m := range mailbox {
			if m == a.Address {
				return true
			}
		}
	}

	return false
}

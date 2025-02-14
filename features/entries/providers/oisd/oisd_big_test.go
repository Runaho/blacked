package oisd

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/config"
	"blacked/internal/utils"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestSetURL(t *testing.T) {
	_, _, _, _ = utils.Initialize(t)

	su := "21.red-80-39-44.staticip.rima-tde.net"
	e := entries.Entry{}
	err := e.SetURL(su)
	log.Info().Any("entry", e).Str("su", su).Msg("entry setted url")
	if err != nil {
		t.Errorf("error setting URL: %v", err)
	}

	if e.Domain != "rima-tde.net" {
		t.Errorf("expected domain rima-tde.net, got %v", e.Domain)
	}

	// e.Host

	// e.SubDomains[]
	subdomains := []string{"21", "red-80-39-44", "staticip"}
	for i, sd := range subdomains {
		if e.SubDomains[i] != sd {
			t.Errorf("expected subdomain %v, got %v", sd, e.SubDomains[i])
		}
	}

	if e.Path != "" {
		t.Errorf("expected path '', got %v", e.Path)
	}

}

func TestSecondURL(t *testing.T) {
	su := "001420990998183-dot-wetransfer-auth-file-342.appspot.com"
	e := entries.Entry{}
	err := e.SetURL(su)
	log.Info().Any("entry", e).Str("su", su).Msg("entry setted url")
	if err != nil {
		t.Errorf("error setting URL: %v", err)
	}

	if e.Domain != "appspot.com" {
		t.Errorf("expected domain appspot.com, got %v", e.Domain)
	}

	if e.Host != "001420990998183-dot-wetransfer-auth-file-342.appspot.com" {
		t.Errorf("expected host 001420990998183-dot-wetransfer-auth-file-342.appspot.com, got %v", e.Host)
	}

	if len(e.SubDomains) != 1 || e.SubDomains[0] != "001420990998183-dot-wetransfer-auth-file-342" {
		t.Errorf("expected subdomains [001420990998183-dot-wetransfer-auth-file-342], got %v", e.SubDomains)
	}

	if e.Path != "" {
		t.Errorf("expected path '', got %v", e.Path)
	}
}

// TestParse checks the OisdBigProvider.Parse() method, ensuring it processes lines properly.
func TestParse(t *testing.T) {
	_, db, cc, err := utils.Initialize(t)
	defer db.Close()
	assert.NoError(t, err, "Expected no error initializing providers")

	repository := repository.NewSQLiteRepository(db)

	provider := NewOISDBigProvider(&config.GetConfig().Collector, cc)
	provider.SetRepository(repository)

	processID := uuid.New()
	startedAt := time.Now()

	source := provider.Source()
	name := provider.Name()
	strProcessID := processID.String()

	log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).Time("starts", startedAt).Msg("start processing data")
	provider.SetProcessID(processID)
	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if meta != nil {
		log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).TimeDiff("duration", time.Now(), startedAt).Msg("There is a meta data for the process changing the process id")
		strProcessID = meta.ProcessID
		provider.SetProcessID(uuid.MustParse(strProcessID))
	}

	assert.NoError(t, err, "Expected no error fetching data")
	assert.NotNil(t, reader, "Expected a non-nil reader")
	if err != nil || reader == nil {
		return
	}

	if err := provider.Parse(reader); err != nil {
		assert.NoError(t, err, "Expected no error parsing data")
		return
	}

	log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).TimeDiff("duration", time.Now(), startedAt).Msg("finished processing data")

}

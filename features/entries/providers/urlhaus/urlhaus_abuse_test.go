package urlhaus

import (
	"blacked/features/entries/repository"
	"blacked/internal/config"
	"blacked/internal/utils"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

// TestParse checks the URLHausProvider.Parse() method, ensuring it processes lines properly.
func TestParse(t *testing.T) {
	_, db, cc, err := utils.Initialize(t)
	defer db.Close()
	assert.NoError(t, err, "Expected no error initializing providers")

	repository := repository.NewDuckDBRepository(db)

	provider := NewURLHausProvider(&config.GetConfig().Collector, cc, repository)

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

package providers

import (
	"blacked/features/entries/providers/oisd"
	"blacked/features/entries/providers/openphish"
	"blacked/features/entries/providers/urlhaus"
	"blacked/features/entries/repository"
	"blacked/internal/collector"
	"blacked/internal/colly"
	"blacked/internal/config"
	"blacked/internal/utils"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type Provider interface {
	Name() string
	Source() string
	Fetch() (io.Reader, error)
	Parse(data io.Reader) error
	SetProcessID(id uuid.UUID)
}

type Providers []Provider

func NewProviders(db *sql.DB) (providers *Providers, err error) {
	cfg := config.GetConfig()
	cc, err := colly.InitCollyClient()
	if err != nil {
		log.Error().Err(err).Msg("error initializing colly client")
		return
	}

	repository := repository.NewDuckDBRepository(db)

	providers = &Providers{
		oisd.NewOISDBigProvider(&cfg.Collector, cc, repository),
		oisd.NewOISDNSFWProvider(&cfg.Collector, cc, repository),
		openphish.NewOpenPhishFeedProvider(&cfg.Collector, cc, repository),
		urlhaus.NewURLHausProvider(&cfg.Collector, cc, repository),
		//&oisd.OISDNSFW{},
		//&phishtank.OnlineValid{},
	}

	srcs := providers.Sources()

	log.Info().Msgf("initialized provider sources: %v", srcs)

	sourceDomains, err := providers.SourceDomains()
	if err != nil {
		log.Error().Err(err).Msg("error getting source domains")
		return
	}
	log.Info().Msgf("initialized provider source domains: %v", sourceDomains)

	cc.AllowedDomains = sourceDomains
	collector.NewMetricsCollector(srcs)

	return
}

func (p Providers) Process() error {
	for _, provider := range p {
		processID := uuid.New()
		startedAt := time.Now()

		source := provider.Source()
		name := provider.Name()
		strProcessID := processID.String()

		log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).Time("starts", startedAt).Msg("start processing data")

		provider.SetProcessID(processID)

		reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
		if err != nil {
			log.Error().Err(err).Str("process_id", strProcessID).Str("source", source).Str("name", name).Msg("error fetching data")
			return fmt.Errorf("error fetching data from %s: %w", provider.Source(), err)
		}

		if meta != nil {
			log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).TimeDiff("duration", time.Now(), startedAt).Msg("There is a meta data for the process changing the process id")
			strProcessID = meta.ProcessID
			provider.SetProcessID(uuid.MustParse(strProcessID))
		}

		if err := provider.Parse(reader); err != nil {
			log.Error().Err(err).Str("process_id", strProcessID).Str("source", source).Str("name", name).Msg("error parsing data")
			return err
		}

		cfg := config.GetConfig()

		if cfg.APP.Environtment != "development" {
			utils.RemoveStoredResponse(name)
		}

		log.Info().Str("process_id", strProcessID).Str("source", source).Str("name", name).TimeDiff("duration", time.Now(), startedAt).Msg("finished processing data")
	}
	return nil
}

func (p Providers) NamesAndSources() map[string]string {
	result := make(map[string]string)
	for _, provider := range p {
		result[provider.Name()] = provider.Source()
	}
	return result
}

func (p Providers) Names() []string {
	var result []string
	for _, provider := range p {
		result = append(result, provider.Name())
	}
	return result
}

func (p Providers) Sources() []string {
	var result []string
	for _, provider := range p {
		result = append(result, provider.Source())
	}

	return result
}

func (p Providers) SourceDomains() (result []string, e error) {
	for _, provider := range p {
		var uri *url.URL
		uri, e = url.Parse(provider.Source())
		if e != nil {
			log.Error().Err(e).Msg("error parsing source url")
			return
		}

		result = append(result, uri.Host)
	}

	uri, e := url.Parse("https://raw.githubusercontent.com")
	if e != nil {
		log.Error().Err(e).Msg("error parsing source url")
	}
	result = append(result, uri.Host)

	return
}

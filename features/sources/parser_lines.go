package sources

import (
	"bufio"
	"io"

	"github.com/rs/zerolog/log"
)

// feedLines reads all lines from r and sends them on ch.
// The caller is responsible for closing ch and waiting for consumers.
func feedLines(r io.Reader, ch chan<- string) error {
	scanner := bufio.NewScanner(r)
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		ch <- scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("Scanner error during line parsing")
		return err
	}
	return nil
}

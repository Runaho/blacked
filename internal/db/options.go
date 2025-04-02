package db

type dbOptions struct {
	isTesting   bool
	isInWALMode bool
	inMemory    bool
}

func (o *dbOptions) GetIsTesting() bool {
	return o.isTesting
}

func (o *dbOptions) GetInMemory() bool {
	return o.inMemory
}

type Option func(*dbOptions)

func WithTesting(state bool) Option {
	return func(opts *dbOptions) {
		opts.isTesting = state
	}
}

func WithInMemory(state bool) Option {
	return func(opts *dbOptions) {
		opts.inMemory = state
	}
}

func WithWALMode(state bool) Option {
	return func(opts *dbOptions) {
		opts.isInWALMode = state
	}
}

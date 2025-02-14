package db

type dbOptions struct {
	isTesting  bool
	isReadOnly bool
	inMemory   bool
}

func (o *dbOptions) GetIsReadOnly() bool {
	return o.isReadOnly
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

func WithReadOnly(state bool) Option {
	return func(opts *dbOptions) {
		opts.isReadOnly = state
	}
}

func WithInMemory(state bool) Option {
	return func(opts *dbOptions) {
		opts.inMemory = state
	}
}

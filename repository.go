package sql

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

type Transactional interface {
	Begin(ctx context.Context) (childCtx context.Context, commit func() error, rollback func() error, err error)
}

type Scannable interface {
	Scan() []any
}

type Returnable interface {
	Returns() []string
}

type ScannableModel[Model any] interface {
	*Model
	Scannable
	Returnable
}

type Mappable interface {
	Mappings() map[string]string
}

type selectable interface {
	Returning(fields ...string) SelectQuery
}

type insertable interface {
	Returning(...string) InsertQuery
}

type updatable interface {
	Returning(fields ...string) UpdateQuery
}

type repositoryConfig struct {
	mappings map[string]string
}

type repositoryOption func(config *repositoryConfig) error

func WithExtraMapping(mapping map[string]string) repositoryOption {
	return func(r *repositoryConfig) error {
		for k, v := range mapping {
			r.mappings[k] = v
		}
		return nil
	}
}

type Repository[Model any] interface {
	Transactional
	Get(ctx context.Context, builder Builder) (*Model, error)
	Select(ctx context.Context, builder Builder) ([]*Model, error)
	Delete(ctx context.Context, builder DeleteBuilder) error
	Count(ctx context.Context, query CountBuilder) (int, error)
}

func NewRepository[Model any, _ ScannableModel[Model]](connection Db, opts ...repositoryOption) (*repository[Model], error) {
	var m Model
	if _, ok := any(m).(Scannable); ok {
		return nil, fmt.Errorf("type %T implements interface sql.Scannable with a value receiver but pointer receiver required", m)
	}
	config := &repositoryConfig{
		mappings: make(map[string]string),
	}
	if mm, ok := any(&m).(Mappable); ok {
		maps.Copy(config.mappings, mm.Mappings())
	}
	for _, opt := range opts {
		if err := opt(config); err != nil {
			return nil, err
		}
	}

	return &repository[Model]{DB: connection, mappings: config.mappings}, nil
}

type repository[Model any] struct {
	DB       Db
	mappings map[string]string
}

func (r *repository[Model]) Begin(ctx context.Context) (context.Context, func() error, func() error, error) {
	return NewContextWithTransaction(ctx, r.DB)
}

func (r *repository[Model]) inTransaction[T any](ctx context.Context, fn func(context.Context, Tx) (T, error)) (result T, err error) {
	tx, err := TransactionFromContext(ctx)
	if err == nil {
		return fn(ctx, tx)
	}
	if !errors.Is(err, ErrNoTransactionInContext) {
		return result, err
	}

	var commit, rollback func() error
	ctx, commit, rollback, err = r.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer commitOrRollback(&err, commit, rollback)

	tx, err = TransactionFromContext(ctx)
	if err != nil {
		return result, err
	}
	return fn(ctx, tx)
}

func applyReturning[Model any](builder Builder, model *Model) {
	returning := any(model).(Returnable).Returns()
	if len(returning) == 0 {
		returning = []string{"*"}
	}
	switch q := builder.(type) {
	case insertable:
		q.Returning(returning...)
	case updatable:
		q.Returning(returning...)
	case selectable:
		q.Returning(returning...)
	}
}

func (r *repository[Model]) compile(builder Builder) (string, []any, error) {
	q, args, err := builder.Build(r.DB.Driver(), WithMappings(r.mappings))
	if err != nil {
		return q, args, fmt.Errorf("%w (%s, %v)", err, q, args)
	}
	args, err = values(args)
	if err != nil {
		return q, nil, err
	}
	return q, args, nil
}

func (r *repository[Model]) returnsRows(builder Builder) bool {
	if r.DB.Driver().Returning() {
		return true
	}
	switch builder.(type) {
	case InsertQuery, UpdateQuery, DeleteQuery:
		return false
	}
	return true
}

func (r *repository[Model]) Get(ctx context.Context, builder Builder) (_ *Model, err error) {
	defer func() {
		err = NewError(err)
	}()
	return r.inTransaction(ctx, func(ctx context.Context, tx Tx) (*Model, error) {
		var model Model
		applyReturning(builder, &model)
		q, args, err := r.compile(builder)
		if err != nil {
			return &model, err
		}

		if !r.returnsRows(builder) {
			_, err = tx.Exec(ctx, q, args...)
			return &model, err
		}
		m := any(&model).(Scannable)
		return &model, tx.QueryRow(ctx, q, args...).Scan(m.Scan()...)
	})
}

func (r *repository[Model]) Select(ctx context.Context, builder Builder) (_ []*Model, err error) {
	defer func() {
		err = NewError(err)
	}()
	return r.inTransaction(ctx, func(ctx context.Context, tx Tx) (models []*Model, err error) {
		applyReturning(builder, new(Model))
		q, args, err := r.compile(builder)
		if err != nil {
			return nil, err
		}

		if !r.returnsRows(builder) {
			_, err = tx.Exec(ctx, q, args...)
			return nil, err
		}
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var model Model
			m := any(&model).(Scannable)
			if err := rows.Scan(m.Scan()...); err != nil {
				return nil, err
			}
			models = append(models, &model)
		}
		return models, nil
	})
}

func (r *repository[Model]) Delete(ctx context.Context, builder DeleteBuilder) (err error) {
	defer func() {
		err = NewError(err)
	}()
	_, err = r.inTransaction(ctx, func(ctx context.Context, tx Tx) (any, error) {
		q, args, err := r.compile(builder)
		if err != nil {
			return nil, err
		}

		_, err = tx.Exec(ctx, q, args...)
		return nil, err
	})
	return err
}

func (r *repository[Model]) Count(ctx context.Context, builder CountBuilder) (_ int, err error) {
	defer func() {
		err = NewError(err)
	}()
	return r.inTransaction(ctx, func(ctx context.Context, tx Tx) (int, error) {
		q, args, err := r.compile(builder)
		if err != nil {
			return 0, err
		}

		var total int
		if err := tx.QueryRow(ctx, q, args...).Scan(&total); err != nil {
			if e, ok := FromError(NewError(err)); ok && e.Is(ErrNoRows) {
				return 0, nil
			}
			return 0, err
		}
		return total, nil
	})
}

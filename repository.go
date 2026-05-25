package sql

import (
	"context"
	"errors"
	"fmt"
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

type selectable interface {
	Returning(fields ...string) SelectQuery
}

type insertable interface {
	Returning(...string) InsertQuery
}

type updatable interface {
	Returning(fields ...string) UpdateQuery
}

type Repository[Model any] interface {
	Transactional
	Get(ctx context.Context, builder Builder) (*Model, error)
	Select(ctx context.Context, builder Builder) ([]*Model, error)
	Delete(ctx context.Context, builder DeleteBuilder) error
	Count(ctx context.Context, query CountBuilder) (int, error)
}

func NewRepository[Model any, _ ScannableModel[Model]](connection Db) (*repository[Model], error) {
	var m Model
	if _, ok := any(m).(Scannable); ok {
		return nil, fmt.Errorf("type %T implements interface sql.Scannable with a value receiver but pointer receiver required", m)
	}
	return &repository[Model]{DB: connection}, nil
}

type repository[Model any] struct {
	DB Db
}

func (r *repository[Model]) Begin(ctx context.Context) (context.Context, func() error, func() error, error) {
	return NewContextWithTransaction(ctx, r.DB)
}

func (r *repository[Model]) Get(ctx context.Context, builder Builder) (_ *Model, err error) {
	defer func() {
		err = NewError(err)
	}()
	var model Model
	var commit, rollback func() error
	tx, err := TransactionFromContext(ctx)
	if errors.Is(err, ErrNoTransactionInContext) {
		ctx, commit, rollback, err = r.Begin(ctx)
		if err != nil {
			return
		}
		defer func() {
			if err == nil {
				err = commit()
			}
			if e := rollback(); err == nil {
				err = e
			}
		}()
		tx, err = TransactionFromContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	ret := any(&model).(Returnable)
	returning := ret.Returns()
	if len(returning) == 0 {
		returning = []string{"*"}
	}
	if ret, ok := builder.(insertable); ok {
		ret.Returning(returning...)
	} else if ret, ok := builder.(updatable); ok {
		ret.Returning(returning...)
	} else if ret, ok := builder.(selectable); ok {
		ret.Returning(returning...)
	}
	q, args, err := builder.Build(r.DB.Driver())
	if err != nil {
		return &model, fmt.Errorf("%w (%s, %v)", err, q, args)
	}
	args, err = values(args)
	if err != nil {
		return &model, err
	}

	_, ok := builder.(InsertQuery)
	if !ok {
		_, ok = builder.(UpdateQuery)
	}
	if !r.DB.Driver().Returning() && ok {
		_, err = tx.Exec(ctx, q, args...)
	} else {
		m := any(&model).(Scannable)
		err = tx.QueryRow(ctx, q, args...).Scan(m.Scan()...)
	}
	return &model, err
}

func (r *repository[Model]) Select(ctx context.Context, builder Builder) (models []*Model, err error) {
	defer func() {
		err = NewError(err)
	}()
	var commit, rollback func() error
	tx, err := TransactionFromContext(ctx)
	if errors.Is(err, ErrNoTransactionInContext) {
		ctx, commit, rollback, err = r.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err == nil {
				err = commit()
			}
			if e := rollback(); err == nil {
				err = e
			}
		}()
		tx, err = TransactionFromContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	returning := any(new(Model)).(Returnable).Returns()
	if len(returning) == 0 {
		returning = []string{"*"}
	}
	if ret, ok := builder.(insertable); ok {
		ret.Returning(returning...)
	} else if ret, ok := builder.(updatable); ok {
		ret.Returning(returning...)
	} else if ret, ok := builder.(selectable); ok {
		ret.Returning(returning...)
	}
	q, args, err := builder.Build(r.DB.Driver())
	if err != nil {
		return nil, fmt.Errorf("%w (%s, %v)", err, q, args)
	}
	args, err = values(args)
	if err != nil {
		return nil, err
	}

	_, ok := builder.(InsertQuery)
	if !ok {
		_, ok = builder.(UpdateQuery)
	}
	if !r.DB.Driver().Returning() && ok {
		_, err = tx.Exec(ctx, q, args...)
		return nil, err
	} else {
		var rows Rows
		rows, err = tx.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var model Model
			m := any(&model).(Scannable)
			err = rows.Scan(m.Scan()...)
			if err != nil {
				return nil, err
			}
			models = append(models, &model)
		}
	}
	return
}

func (r *repository[Model]) Delete(ctx context.Context, builder DeleteBuilder) (err error) {
	defer func() {
		err = NewError(err)
	}()
	var commit, rollback func() error
	tx, err := TransactionFromContext(ctx)
	if errors.Is(err, ErrNoTransactionInContext) {
		ctx, commit, rollback, err = r.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() {
			if err == nil {
				err = commit()
			}
			if e := rollback(); err == nil {
				err = e
			}
		}()
		tx, err = TransactionFromContext(ctx)
		if err != nil {
			return err
		}
	}

	q, args, err := builder.Build(r.DB.Driver())
	if err != nil {
		return err
	}
	args, err = values(args)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, q, args...)
	return err
}

func (r *repository[Model]) Count(ctx context.Context, builder CountBuilder) (total int, err error) {
	defer func() {
		err = NewError(err)
	}()
	var commit, rollback func() error
	tx, err := TransactionFromContext(ctx)
	if errors.Is(err, ErrNoTransactionInContext) {
		ctx, commit, rollback, err = r.Begin(ctx)
		if err != nil {
			return 0, err
		}
		defer func() {
			if err == nil {
				err = commit()
			}
			if e := rollback(); err == nil {
				err = e
			}
		}()
		tx, err = TransactionFromContext(ctx)
		if err != nil {
			return 0, err
		}
	}

	q, args, err := builder.Build(r.DB.Driver())
	if err != nil {
		return 0, err
	}
	args, err = values(args)
	if err != nil {
		return 0, err
	}

	err = tx.QueryRow(ctx, q, args...).Scan(&total)
	if err != nil {
		if e, ok := FromError(NewError(err)); ok && e.Is(ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	return
}

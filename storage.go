package sql

import (
	"context"

	"gitlab.com/gorib/criteria"
)

type Storage[Model any] interface {
	View[Model]
	Create(ctx context.Context, model *Model) (*Model, error)
	Update(ctx context.Context, model *Model) (*Model, error)

	Remove(ctx context.Context, expr criteria.Expression) error
}

type View[Model any] interface {
	Repository() Repository[Model]
	Get(ctx context.Context, expr criteria.Expression) (*Model, error)
	Find(ctx context.Context, expr criteria.Expression) ([]*Model, error)
	Count(ctx context.Context, expr criteria.Expression) (int, error)
}

func NewStorage[Model any, scan ScannableModel[Model]](connection Db, from *from, opts ...repositoryOption) (*storage[Model], error) {
	repo, err := NewRepository[Model, scan](connection, opts...)
	if err != nil {
		return nil, err
	}
	return &storage[Model]{repo: repo, source: from}, nil
}

type storage[Model any] struct {
	repo   *repository[Model]
	source *from
}

func (s *storage[Model]) Repository() Repository[Model] {
	return s.repo
}

func (s *storage[Model]) Get(ctx context.Context, expr criteria.Expression) (*Model, error) {
	return s.repo.Get(ctx, Select().From(s.source).Where(expr))
}

func (s *storage[Model]) Remove(ctx context.Context, expr criteria.Expression) error {
	return s.repo.Delete(ctx, Delete().From(s.source).Where(expr))
}

// Code generated by pg-bindings generator. DO NOT EDIT.

//go:build sql_integration

package postgres

import (
	"context"
	"testing"

	"github.com/stackrox/rox/generated/storage"
	"github.com/stackrox/rox/pkg/env"
	"github.com/stackrox/rox/pkg/postgres/pgtest"
	"github.com/stackrox/rox/pkg/sac"
	"github.com/stackrox/rox/pkg/testutils"
	"github.com/stackrox/rox/pkg/testutils/envisolator"
	"github.com/stretchr/testify/suite"
)

type TestGGrandChild1StoreSuite struct {
	suite.Suite
	envIsolator *envisolator.EnvIsolator
	store       Store
	testDB      *pgtest.TestPostgres
}

func TestTestGGrandChild1Store(t *testing.T) {
	suite.Run(t, new(TestGGrandChild1StoreSuite))
}

func (s *TestGGrandChild1StoreSuite) SetupSuite() {
	s.envIsolator = envisolator.NewEnvIsolator(s.T())
	s.envIsolator.Setenv(env.PostgresDatastoreEnabled.EnvVar(), "true")

	if !env.PostgresDatastoreEnabled.BooleanSetting() {
		s.T().Skip("Skip postgres store tests")
		s.T().SkipNow()
	}

	s.testDB = pgtest.ForT(s.T())
	s.store = New(s.testDB.Pool)
}

func (s *TestGGrandChild1StoreSuite) SetupTest() {
	ctx := sac.WithAllAccess(context.Background())
	tag, err := s.testDB.Exec(ctx, "TRUNCATE test_g_grand_child1 CASCADE")
	s.T().Log("test_g_grand_child1", tag)
	s.NoError(err)
}

func (s *TestGGrandChild1StoreSuite) TearDownSuite() {
	s.testDB.Teardown(s.T())
	s.envIsolator.RestoreAll()
}

func (s *TestGGrandChild1StoreSuite) TestStore() {
	ctx := sac.WithAllAccess(context.Background())

	store := s.store

	testGGrandChild1 := &storage.TestGGrandChild1{}
	s.NoError(testutils.FullInit(testGGrandChild1, testutils.SimpleInitializer(), testutils.JSONFieldsFilter))

	foundTestGGrandChild1, exists, err := store.Get(ctx, testGGrandChild1.GetId())
	s.NoError(err)
	s.False(exists)
	s.Nil(foundTestGGrandChild1)

	withNoAccessCtx := sac.WithNoAccess(ctx)

	s.NoError(store.Upsert(ctx, testGGrandChild1))
	foundTestGGrandChild1, exists, err = store.Get(ctx, testGGrandChild1.GetId())
	s.NoError(err)
	s.True(exists)
	s.Equal(testGGrandChild1, foundTestGGrandChild1)

	testGGrandChild1Count, err := store.Count(ctx)
	s.NoError(err)
	s.Equal(1, testGGrandChild1Count)
	testGGrandChild1Count, err = store.Count(withNoAccessCtx)
	s.NoError(err)
	s.Zero(testGGrandChild1Count)

	testGGrandChild1Exists, err := store.Exists(ctx, testGGrandChild1.GetId())
	s.NoError(err)
	s.True(testGGrandChild1Exists)
	s.NoError(store.Upsert(ctx, testGGrandChild1))
	s.ErrorIs(store.Upsert(withNoAccessCtx, testGGrandChild1), sac.ErrResourceAccessDenied)

	foundTestGGrandChild1, exists, err = store.Get(ctx, testGGrandChild1.GetId())
	s.NoError(err)
	s.True(exists)
	s.Equal(testGGrandChild1, foundTestGGrandChild1)

	s.NoError(store.Delete(ctx, testGGrandChild1.GetId()))
	foundTestGGrandChild1, exists, err = store.Get(ctx, testGGrandChild1.GetId())
	s.NoError(err)
	s.False(exists)
	s.Nil(foundTestGGrandChild1)
	s.NoError(store.Delete(withNoAccessCtx, testGGrandChild1.GetId()))

	var testGGrandChild1s []*storage.TestGGrandChild1
	for i := 0; i < 200; i++ {
		testGGrandChild1 := &storage.TestGGrandChild1{}
		s.NoError(testutils.FullInit(testGGrandChild1, testutils.UniqueInitializer(), testutils.JSONFieldsFilter))
		testGGrandChild1s = append(testGGrandChild1s, testGGrandChild1)
	}

	s.NoError(store.UpsertMany(ctx, testGGrandChild1s))

	testGGrandChild1Count, err = store.Count(ctx)
	s.NoError(err)
	s.Equal(200, testGGrandChild1Count)
}

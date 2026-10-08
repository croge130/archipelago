package sdk

import (
	aliasFacade "github.com/croge130/archipelago/alias/facade"
	certstoreFacade "github.com/croge130/archipelago/certstore/facade"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	policyFacade "github.com/croge130/archipelago/policy/facade"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
)

// Each base's Reader and Writer are held as the base's own facade
// interfaces — never a concrete storage type — and each side is
// independently optional. A nil Writer means read-only for that base; a
// nil Reader (and Writer) means the node has no access to that base at
// all. See 17-sdk-model.md for the access shapes this has to tolerate.

type GatehouseStores struct {
	Reader gatehouseFacade.Reader
	Writer gatehouseFacade.Writer
}

type PolicyStores struct {
	Reader policyFacade.Reader
	Writer policyFacade.Writer
}

type AliasStores struct {
	Reader aliasFacade.Reader
	Writer aliasFacade.Writer
}

type CertStores struct {
	Reader certstoreFacade.Reader
	Writer certstoreFacade.Writer
}

type JobsStores struct {
	Reader jobsFacade.Reader
	Writer jobsFacade.Writer
}

type VitalsStores struct {
	Reader vitalsFacade.Reader
	Writer vitalsFacade.Writer
}

// Stores is every store a node might hold, one pair per base. Which
// entries must be populated depends on the Modes enabled; see New.
type Stores struct {
	Gatehouse GatehouseStores
	Policy    PolicyStores
	Alias     AliasStores
	Certs     CertStores
	Vitals    VitalsStores
	Jobs      JobsStores
}

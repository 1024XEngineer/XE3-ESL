package port

import "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"

// Application services own these persistence/source ports. The aliases keep
// the current domain command types stable while callers migrate to this layer.
type ProfileRepository = preparation.ProfileRepository
type ProfileSnapshotReader = preparation.ProfileSnapshotReader
type JobTargetRepository = preparation.JobTargetRepository
type PlanRepository = preparation.PlanRepository
type PlanReader = preparation.PlanReader
type ResumeRevisionReader = preparation.ResumeRevisionReader
type ResourceIDGenerator = preparation.ResourceIDGenerator
type SourceThreadReader = preparation.SourceThreadReader
type PolicyResolver = preparation.PolicyResolver

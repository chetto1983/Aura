//go:build db_integration

package db

// MigrationStepsAbove counts migrations newer than version for integration fixtures.
// The embedded sequence has gaps, so golang-migrate's step count is not head-version.
func MigrationStepsAbove(version int64) (int, error) {
	versions, err := migrationVersions()
	if err != nil {
		return 0, err
	}
	steps := 0
	for _, candidate := range versions {
		if candidate > version {
			steps++
		}
	}
	return steps, nil
}

// MigrationStepDelta returns the signed MigrateSteps distance between two fixture states.
func MigrationStepDelta(from, to int64) (int, error) {
	aboveFrom, err := MigrationStepsAbove(from)
	if err != nil {
		return 0, err
	}
	aboveTo, err := MigrationStepsAbove(to)
	if err != nil {
		return 0, err
	}
	return aboveFrom - aboveTo, nil
}

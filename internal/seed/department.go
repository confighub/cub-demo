package seed

// PlatformDepartment is the Department label of everything a scenario marks
// as shared: the platform components and their bases, the shared clusters,
// and the deployments on them. In the scenario language "shared" (or no
// department) means "belongs to no app team"; in the org that is the
// platform team, so the label says so instead of being left off.
const PlatformDepartment = "platform"

// departmentLabel is the Department label for a scenario department.
func departmentLabel(dept string) string {
	if dept == "" || dept == "shared" {
		return PlatformDepartment
	}
	return dept
}

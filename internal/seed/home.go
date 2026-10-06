package seed

import "fmt"

// home creates the home space, which holds what belongs to the demo as a
// whole rather than to a cluster or a component: the ChangeWorkflows and the
// org-wide Filters and Views.
func (s *Seeder) home() error {
	m := s.Model
	_, created, err := s.ensureSpace(m.Home, m.Scenario.Company+" platform", s.baseLabels(map[string]string{"Layer": "demo"}), nil)
	if err != nil {
		return err
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure home space %s\n", m.Home)
		return nil
	}
	if created {
		fmt.Fprintf(s.Out, "  created home space %s\n", m.Home)
	}
	return nil
}

package container_test

import (
	"fmt"
	"regexp"
	"testing"

	r "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/synology-community/terraform-provider-synology/synology/acctest"
)

// The tofuacc- prefix lets scripts/synology/sweep-acctest.sh find leftovers
// if a run dies mid-apply; the project lives on the tofuacc scratch share.
const accProjectName = "tofuacc-plat902"

func projectUpdateAccConfig(image string) string {
	return fmt.Sprintf(`
resource "synology_container_project" "test" {
  name       = %[1]q
  share_path = "/tofuacc/%[1]s"
  run        = true

  services = {
    app = {
      image   = %[2]q
      command = ["sleep", "infinity"]
    }
  }
}
`, accProjectName, image)
}

// TestAccProjectResource_servicesUpdateReachesDSM is the PLAT-902 regression
// test. Changing services on a running project with content omitted must
// change the compose DSM stores. Content in state is read back from DSM, so
// the step-2 check fails if DSM kept the old compose, and the framework's
// post-apply plan fails if DSM's stored content differs from what the
// provider renders.
func TestAccProjectResource_servicesUpdateReachesDSM(t *testing.T) {
	r.Test(t, r.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(t),
		Steps: []r.TestStep{
			{
				Config: projectUpdateAccConfig("busybox:1.36"),
				Check: r.TestMatchResourceAttr(
					"synology_container_project.test",
					"content",
					regexp.MustCompile(`image: busybox:1\.36\b`),
				),
			},
			{
				Config: projectUpdateAccConfig("busybox:1.37"),
				Check: r.TestMatchResourceAttr(
					"synology_container_project.test",
					"content",
					regexp.MustCompile(`image: busybox:1\.37\b`),
				),
			},
		},
	})
}

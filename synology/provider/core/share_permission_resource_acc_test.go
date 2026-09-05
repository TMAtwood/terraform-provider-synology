package core_test

import (
	"fmt"
	"testing"

	r "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/synology-community/terraform-provider-synology/synology/acctest"
)

// Names use the tofuacc- prefix so scripts/synology/sweep-acctest.sh can
// find leftovers if a run dies mid-apply. The share is created by this
// test rather than reusing the tofuacc scratch share or photo/homes:
// synology_core_share_permission owns the full (share, user_group_type)
// list, and Delete revokes every non-admin row.
//
// DSM lists every local account on every share, most as all-flags-false.
// Those rows enter state, so config must declare them -- the same closed
// list the infra repo writes for photo/homes. A one-block config fails
// after apply with extra set elements (PLAT-741, live NAS).
func TestAccSharePermissionResource_basic(t *testing.T) {
	share := "tofuacc-sp741"
	userA := "tofuacc-u741a"
	userB := "tofuacc-u741b"

	r.Test(t, r.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(t),
		Steps: []r.TestStep{
			{
				Config: sharePermissionAccConfig(share, userA, userB, `
  permission {
    name        = synology_core_user.a.name
    is_writable = true
  }
  permission { name = synology_core_user.b.name }
`+nasLocalUsersNoAccess),
				Check: r.ComposeTestCheckFunc(
					r.TestCheckResourceAttr("synology_core_share_permission.test", "share", share),
					r.TestCheckResourceAttr("synology_core_share_permission.test", "user_group_type", "local_user"),
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userA,
						"is_writable": "true",
						"is_readonly": "false",
						"is_deny":     "false",
						"is_custom":   "false",
					}),
				),
			},
			{
				Config: sharePermissionAccConfig(share, userA, userB, `
  permission {
    name         = synology_core_user.a.name
    is_readonly  = true
    is_writable  = false
  }
  permission { name = synology_core_user.b.name }
`+nasLocalUsersNoAccess),
				Check: r.ComposeTestCheckFunc(
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userA,
						"is_writable": "false",
						"is_readonly": "true",
					}),
				),
			},
			{
				Config: sharePermissionAccConfig(share, userA, userB, `
  permission {
    name         = synology_core_user.a.name
    is_readonly  = true
  }
  permission {
    name        = synology_core_user.b.name
    is_writable = true
  }
`+nasLocalUsersNoAccess),
				Check: r.ComposeTestCheckFunc(
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userA,
						"is_readonly": "true",
						"is_writable": "false",
					}),
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userB,
						"is_writable": "true",
						"is_readonly": "false",
					}),
				),
			},
			{
				Config: sharePermissionAccConfig(share, userA, userB, `
  permission {
    name         = synology_core_user.a.name
    is_readonly  = true
  }
  permission { name = synology_core_user.b.name }
`+nasLocalUsersNoAccess),
				Check: r.ComposeTestCheckFunc(
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userA,
						"is_readonly": "true",
						"is_writable": "false",
					}),
					r.TestCheckTypeSetElemNestedAttrs("synology_core_share_permission.test", "permission.*", map[string]string{
						"name":        userB,
						"is_writable": "false",
						"is_readonly": "false",
					}),
				),
			},
			{
				ResourceName:                         "synology_core_share_permission.test",
				ImportState:                          true,
				ImportStateId:                        share + "/local_user",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "share",
			},
		},
	})
}

// Closed local-user list on the production DS1621+ (minus administrators,
// which the resource excludes). Measured 2026-09-04 during PLAT-741.
const nasLocalUsersNoAccess = `
  permission { name = "guest" }
  permission { name = "homes" }
  permission { name = "jeatwood" }
  permission { name = "paatwood" }
  permission { name = "scanner" }
  permission { name = "windows_drives" }
`

func sharePermissionAccConfig(share, userA, userB, permissionBlocks string) string {
	return fmt.Sprintf(`
resource "synology_core_share" "test" {
  name     = %q
  vol_path = "/volume1"
  desc     = "plat741 acctest"
}

resource "synology_core_user" "a" {
  name                = %q
  password_wo         = "TfAcc!Passw0rd"
  password_wo_version = 1
  description         = "plat741 acctest"
  depends_on          = [synology_core_share.test]
}

resource "synology_core_user" "b" {
  name                = %q
  password_wo         = "TfAcc!Passw0rd"
  password_wo_version = 1
  description         = "plat741 acctest"
  depends_on          = [synology_core_user.a]
}

resource "synology_core_share_permission" "test" {
  share           = synology_core_share.test.name
  user_group_type = "local_user"
%s
}
`, share, userA, userB, permissionBlocks)
}

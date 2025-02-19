package vault

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-vault/util"
	"github.com/hashicorp/vault/api"
)

var (
	approleAuthBackendRoleBackendFromPathRegex = regexp.MustCompile("^auth/(.+)/role/.+$")
	approleAuthBackendRoleNameFromPathRegex    = regexp.MustCompile("^auth/.+/role/(.+)$")
)

func approleAuthBackendRoleResource() *schema.Resource {
	fields := map[string]*schema.Schema{
		"role_name": {
			Type:        schema.TypeString,
			Required:    true,
			Description: "Name of the role.",
			ForceNew:    true,
		},
		"role_id": {
			Type:        schema.TypeString,
			Optional:    true,
			Computed:    true,
			Description: "The RoleID of the role. Autogenerated if not set.",
		},
		"bind_secret_id": {
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     true,
			Description: "Whether or not to require secret_id to be present when logging in using this AppRole.",
		},
		"bound_cidr_list": {
			Type:        schema.TypeSet,
			Optional:    true,
			Description: "List of CIDR blocks that can log in using the AppRole.",
			Elem: &schema.Schema{
				Type: schema.TypeString,
			},
			Deprecated:    "use `secret_id_bound_cidrs` instead",
			ConflictsWith: []string{"secret_id_bound_cidrs"},
		},
		"secret_id_bound_cidrs": {
			Type:        schema.TypeSet,
			Optional:    true,
			Description: "List of CIDR blocks that can log in using the AppRole.",
			Elem: &schema.Schema{
				Type: schema.TypeString,
			},
			ConflictsWith: []string{"bound_cidr_list"},
		},
		"secret_id_num_uses": {
			Type:        schema.TypeInt,
			Optional:    true,
			Description: "Number of times which a particular SecretID can be used to fetch a token from this AppRole, after which the SecretID will expire. Leaving this unset or setting it to 0 will allow unlimited uses.",
		},
		"secret_id_ttl": {
			Type:        schema.TypeInt,
			Optional:    true,
			Description: "Number of seconds a SecretID remains valid for.",
		},
		"backend": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Unique name of the auth backend to configure.",
			ForceNew:    true,
			Default:     "approle",
			// standardise on no beginning or trailing slashes
			StateFunc: func(v interface{}) string {
				return strings.Trim(v.(string), "/")
			},
		},

		// Deprecated
		"policies": {
			Type:     schema.TypeSet,
			Optional: true,
			Elem: &schema.Schema{
				Type: schema.TypeString,
			},
			Description:   "Policies to be set on tokens issued using this AppRole.",
			Deprecated:    "use `token_policies` instead if you are running Vault >= 1.2",
			ConflictsWith: []string{"token_policies"},
		},
		"period": {
			Type:          schema.TypeInt,
			Optional:      true,
			Description:   "Number of seconds to set the TTL to for issued tokens upon renewal. Makes the token a periodic token, which will never expire as long as it is renewed before the TTL each period.",
			Deprecated:    "use `token_period` instead if you are running Vault >= 1.2",
			ConflictsWith: []string{"token_period"},
		},
	}

	addTokenFields(fields, &addTokenFieldsConfig{
		TokenPoliciesConflict: []string{"policies"},
		TokenPeriodConflict:   []string{"period"},
	})

	return &schema.Resource{
		Create: approleAuthBackendRoleCreate,
		Read:   approleAuthBackendRoleRead,
		Update: approleAuthBackendRoleUpdate,
		Delete: approleAuthBackendRoleDelete,
		Exists: approleAuthBackendRoleExists,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		Schema: fields,
	}
}

func approleAuthBackendRoleUpdateFields(d *schema.ResourceData, data map[string]interface{}, create bool) {
	updateTokenFields(d, data, create)

	if create {
		if v, ok := d.GetOkExists("bind_secret_id"); ok {
			data["bind_secret_id"] = v.(bool)
		}

		if v, ok := d.GetOk("secret_id_num_uses"); ok {
			data["secret_id_num_uses"] = v.(int)
		}

		if v, ok := d.GetOk("secret_id_ttl"); ok {
			data["secret_id_ttl"] = v.(int)
		}

		if v, ok := d.GetOk("secret_id_bound_cidrs"); ok {
			data["secret_id_bound_cidrs"] = v.(*schema.Set).List()
		}

		// Deprecated Fields
		if v, ok := d.GetOk("period"); ok {
			data["period"] = v.(int)
		}

		if v, ok := d.GetOk("policies"); ok {
			data["policies"] = v.(*schema.Set).List()
		}

		if v, ok := d.GetOk("bound_cidr_list"); ok {
			data["bound_cidr_list"] = v.(*schema.Set).List()
		}
	} else {
		if d.HasChange("bind_secret_id") {
			data["bind_secret_id"] = d.Get("bind_secret_id").(bool)
		}

		if d.HasChange("secret_id_num_uses") {
			data["secret_id_num_uses"] = d.Get("secret_id_num_uses").(int)
		}

		if d.HasChange("secret_id_ttl") {
			data["secret_id_ttl"] = d.Get("secret_id_ttl").(int)
		}

		if d.HasChange("secret_id_bound_cidrs") {
			data["secret_id_bound_cidrs"] = d.Get("secret_id_bound_cidrs").(*schema.Set).List()
		}

		// Deprecated Fields
		if d.HasChange("period") {
			data["period"] = d.Get("period").(int)
		}

		if d.HasChange("policies") {
			data["policies"] = d.Get("policies").(*schema.Set).List()
		}

		if d.HasChange("bound_cidr_list") {
			data["bound_cidr_list"] = d.Get("bound_cidr_list").(*schema.Set).List()
		}
	}
}

func approleAuthBackendRoleCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*api.Client)

	backend := d.Get("backend").(string)
	role := d.Get("role_name").(string)

	path := approleAuthBackendRolePath(backend, role)

	log.Printf("[DEBUG] Writing AppRole auth backend role %q", path)

	data := map[string]interface{}{}
	approleAuthBackendRoleUpdateFields(d, data, true)

	_, err := client.Logical().Write(path, data)
	if err != nil {
		return fmt.Errorf("error writing AppRole auth backend role %q: %s", path, err)
	}
	d.SetId(path)
	log.Printf("[DEBUG] Wrote AppRole auth backend role %q", path)

	if v, ok := d.GetOk("role_id"); ok {
		log.Printf("[DEBUG] Writing AppRole auth backend role %q RoleID", path)
		_, err := client.Logical().Write(path+"/role-id", map[string]interface{}{
			"role_id": v.(string),
		})
		if err != nil {
			return fmt.Errorf("error writing AppRole auth backend role %q's RoleID: %s", path, err)
		}
		log.Printf("[DEBUG] Wrote AppRole auth backend role %q RoleID", path)
	}

	return approleAuthBackendRoleRead(d, meta)
}

func approleAuthBackendRoleRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*api.Client)
	path := d.Id()

	backend, err := approleAuthBackendRoleBackendFromPath(path)
	if err != nil {
		return fmt.Errorf("invalid path %q for AppRole auth backend role: %s", path, err)
	}

	role, err := approleAuthBackendRoleNameFromPath(path)
	if err != nil {
		return fmt.Errorf("invalid path %q for AppRole auth backend role: %s", path, err)
	}

	log.Printf("[DEBUG] Reading AppRole auth backend role %q", path)
	resp, err := client.Logical().Read(path)
	if err != nil {
		return fmt.Errorf("error reading AppRole auth backend role %q: %s", path, err)
	}
	log.Printf("[DEBUG] Read AppRole auth backend role %q", path)
	if resp == nil {
		log.Printf("[WARN] AppRole auth backend role %q not found, removing from state", path)
		d.SetId("")
		return nil
	}

	d.Set("backend", backend)
	d.Set("role_name", role)
	readTokenFields(d, resp)

	// Backward Compatability for Vault < 1.2
	// Check if the user is using the deprecated `bound_cidr_list`
	if _, deprecated := d.GetOk("bound_cidr_list"); deprecated {
		if v, ok := resp.Data["bound_cidr_list"]; ok {
			d.Set("bound_cidr_list", v)
		} else if v, ok := resp.Data["secret_id_bound_cidrs"]; ok {
			d.Set("bound_cidr_list", v)
		}
	} else {
		if v, ok := resp.Data["secret_id_bound_cidrs"]; ok {
			d.Set("secret_id_bound_cidrs", v)
		}
	}

	// Check if the user is using the deprecated `policies`
	if _, deprecated := d.GetOk("policies"); deprecated {
		// Then we see if `token_policies` was set and unset it
		// Vault will still return `policies`
		if _, ok := d.GetOk("token_policies"); ok {
			d.Set("token_policies", nil)
		}
		if v, ok := resp.Data["policies"]; ok {
			d.Set("policies", v)
		}
	}

	// Check if the user is using the deprecated `period`
	if _, deprecated := d.GetOk("period"); deprecated {
		// Then we see if `token_period` was set and unset it
		// Vault will still return `period`
		if _, ok := d.GetOk("token_period"); ok {
			d.Set("token_period", nil)
		}
		if v, ok := resp.Data["period"]; ok {
			d.Set("period", v)
		}
	}

	for _, k := range []string{"bind_secret_id", "secret_id_num_uses", "secret_id_ttl"} {
		if err := d.Set(k, resp.Data[k]); err != nil {
			return fmt.Errorf("error setting state key \"%s\": %s", k, err)
		}
	}

	log.Printf("[DEBUG] Reading AppRole auth backend role %q RoleID", path)
	resp, err = client.Logical().Read(path + "/role-id")
	if err != nil {
		return fmt.Errorf("error reading AppRole auth backend role %q RoleID: %s", path, err)
	}
	log.Printf("[DEBUG] Read AppRole auth backend role %q RoleID", path)
	if resp != nil {
		d.Set("role_id", resp.Data["role_id"])
	}

	return nil
}

func approleAuthBackendRoleUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*api.Client)
	path := d.Id()

	log.Printf("[DEBUG] Updating AppRole auth backend role %q", path)

	data := map[string]interface{}{}
	approleAuthBackendRoleUpdateFields(d, data, false)

	_, err := client.Logical().Write(path, data)

	d.SetId(path)

	if err != nil {
		return fmt.Errorf("error updating AppRole auth backend role %q: %s", path, err)
	}
	log.Printf("[DEBUG] Updated AppRole auth backend role %q", path)

	if d.HasChange("role_id") {
		log.Printf("[DEBUG] Updating AppRole auth backend role %q RoleID", path)
		_, err := client.Logical().Write(path+"/role-id", map[string]interface{}{
			"role_id": d.Get("role_id").(string),
		})
		if err != nil {
			return fmt.Errorf("error updating AppRole auth backend role %q's RoleID: %s", path, err)
		}
		log.Printf("[DEBUG] Updated AppRole auth backend role %q RoleID", path)
	}

	return approleAuthBackendRoleRead(d, meta)

}

func approleAuthBackendRoleDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*api.Client)
	path := d.Id()

	log.Printf("[DEBUG] Deleting AppRole auth backend role %q", path)
	_, err := client.Logical().Delete(path)
	if err != nil && !util.Is404(err) {
		return fmt.Errorf("error deleting AppRole auth backend role %q", path)
	} else if err != nil {
		log.Printf("[DEBUG] AppRole auth backend role %q not found, removing from state", path)
		d.SetId("")
		return nil
	}
	log.Printf("[DEBUG] Deleted AppRole auth backend role %q", path)

	return nil
}

func approleAuthBackendRoleExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	client := meta.(*api.Client)

	path := d.Id()
	log.Printf("[DEBUG] Checking if AppRole auth backend role %q exists", path)

	resp, err := client.Logical().Read(path)
	if err != nil {
		return true, fmt.Errorf("error checking if AppRole auth backend role %q exists: %s", path, err)
	}
	log.Printf("[DEBUG] Checked if AppRole auth backend role %q exists", path)

	return resp != nil, nil
}

func approleAuthBackendRolePath(backend, role string) string {
	return "auth/" + strings.Trim(backend, "/") + "/role/" + strings.Trim(role, "/")
}

func approleAuthBackendRoleNameFromPath(path string) (string, error) {
	if !approleAuthBackendRoleNameFromPathRegex.MatchString(path) {
		return "", fmt.Errorf("no role found")
	}
	res := approleAuthBackendRoleNameFromPathRegex.FindStringSubmatch(path)
	if len(res) != 2 {
		return "", fmt.Errorf("unexpected number of matches (%d) for role", len(res))
	}
	return res[1], nil
}

func approleAuthBackendRoleBackendFromPath(path string) (string, error) {
	if !approleAuthBackendRoleBackendFromPathRegex.MatchString(path) {
		return "", fmt.Errorf("no backend found")
	}
	res := approleAuthBackendRoleBackendFromPathRegex.FindStringSubmatch(path)
	if len(res) != 2 {
		return "", fmt.Errorf("unexpected number of matches (%d) for backend", len(res))
	}
	return res[1], nil
}

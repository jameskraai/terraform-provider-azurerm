package datashare

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/datashare/mgmt/2019-11-01/datashare"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/location"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/datashare/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/datashare/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceDataShareAccount() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceDataShareAccountCreate,
		Read:   resourceDataShareAccountRead,
		Update: resourceDataShareAccountUpdate,
		Delete: resourceDataShareAccountDelete,

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Importer: pluginsdk.ImporterValidatingResourceId(func(id string) error {
			_, err := parse.AccountID(id)
			return err
		}),

		Schema: map[string]*pluginsdk.Schema{
			"name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.AccountName(),
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"location": azure.SchemaLocation(),

			"identity": {
				Type:     pluginsdk.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"type": {
							Type:     pluginsdk.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(datashare.SystemAssigned),
							}, false),
						},
						"principal_id": {
							Type:     pluginsdk.TypeString,
							Computed: true,
						},
						"tenant_id": {
							Type:     pluginsdk.TypeString,
							Computed: true,
						},
					},
				},
			},

			// the api will save and return the tag keys in lowercase, so an extra validation of the key is all in lowercase is added
			// issue has been created https://github.com/Azure/azure-rest-api-specs/issues/9280
			"tags": tags.SchemaEnforceLowerCaseKeys(),
		},
	}
}

func resourceDataShareAccountCreate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataShare.AccountClient
	subscriptionId := meta.(*clients.Client).Account.SubscriptionId
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id := parse.NewAccountID(subscriptionId, d.Get("resource_group_name").(string), d.Get("name").(string))

	existing, err := client.Get(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		if !utils.ResponseWasNotFound(existing.Response) {
			return fmt.Errorf("checking for present of existing %s: %+v", id, err)
		}
	}
	if existing.ID != nil && *existing.ID != "" {
		return tf.ImportAsExistsError("azurerm_data_share_account", *existing.ID)
	}

	account := datashare.Account{
		Name:     utils.String(id.Name),
		Location: utils.String(location.Normalize(d.Get("location").(string))),
		Identity: expandAzureRmDataShareAccountIdentity(d.Get("identity").([]interface{})),
		Tags:     tags.Expand(d.Get("tags").(map[string]interface{})),
	}

	future, err := client.Create(ctx, id.ResourceGroup, id.Name, account)
	if err != nil {
		return fmt.Errorf("creating %s: %+v", id, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("waiting on creating future for %s: %+v", id, err)
	}

	d.SetId(id.ID())

	return resourceDataShareAccountRead(d, meta)
}

func resourceDataShareAccountRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataShare.AccountClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.AccountID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[INFO] DataShare %q does not exist - removing from state", d.Id())
			d.SetId("")
			return nil
		}
		return fmt.Errorf("retrieving DataShare Account %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}
	d.Set("name", id.Name)
	d.Set("resource_group_name", id.ResourceGroup)
	d.Set("location", location.NormalizeNilable(resp.Location))
	if err := d.Set("identity", flattenAzureRmDataShareAccountIdentity(resp.Identity)); err != nil {
		return fmt.Errorf("setting `identity`: %+v", err)
	}

	return tags.FlattenAndSet(d, resp.Tags)
}

func resourceDataShareAccountUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataShare.AccountClient
	ctx, cancel := timeouts.ForUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.AccountID(d.Id())
	if err != nil {
		return err
	}

	props := datashare.AccountUpdateParameters{}

	if d.HasChange("tags") {
		props.Tags = tags.Expand(d.Get("tags").(map[string]interface{}))
	}

	if _, err = client.Update(ctx, id.ResourceGroup, id.Name, props); err != nil {
		return fmt.Errorf("updating DataShare Account %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	return resourceDataShareAccountRead(d, meta)
}

func resourceDataShareAccountDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataShare.AccountClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.AccountID(d.Id())
	if err != nil {
		return err
	}

	future, err := client.Delete(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		return fmt.Errorf("deleting DataShare Account %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("waiting for DataShare Account %q (Resource Group %q) to be deleted: %+v", id.Name, id.ResourceGroup, err)
	}
	return nil
}

func expandAzureRmDataShareAccountIdentity(input []interface{}) *datashare.Identity {
	identity := input[0].(map[string]interface{})
	return &datashare.Identity{
		Type: datashare.Type(identity["type"].(string)),
	}
}

func flattenAzureRmDataShareAccountIdentity(identity *datashare.Identity) []interface{} {
	if identity == nil {
		return make([]interface{}, 0)
	}

	var principalId, tenantId string

	if identity.PrincipalID != nil {
		principalId = *identity.PrincipalID
	}
	if identity.TenantID != nil {
		tenantId = *identity.TenantID
	}

	return []interface{}{
		map[string]interface{}{
			"type":         string(identity.Type),
			"principal_id": principalId,
			"tenant_id":    tenantId,
		},
	}
}

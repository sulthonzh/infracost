package aws

import (
	"github.com/infracost/infracost/internal/resources"
	"github.com/infracost/infracost/internal/schema"

	"github.com/shopspring/decimal"
)

type AcmCertificate struct {
	Address                 string
	Region                  string
	CertificateAuthorityARN string
}

var AcmCertificateUsageSchema = []*schema.UsageItem{}

func (r *AcmCertificate) PopulateUsage(u *schema.UsageData) {
	resources.PopulateArgsWithUsage(r, u)
}

func (r *AcmCertificate) BuildResource() *schema.Resource {
	if r.CertificateAuthorityARN == "" {
		return &schema.Resource{
			Name:        r.Address,
			NoPrice:     true,
			IsSkipped:   true,
			UsageSchema: AcmCertificateUsageSchema,
		}
	}

	certAuthority := &AcmpcaCertificateAuthority{
		Region: r.Region,
	}

	certCostComponent := certAuthority.certificateCostComponent("Certificate", "0", decimalPtr(decimal.NewFromInt(1)))

	return &schema.Resource{
		Name:           r.Address,
		CostComponents: []*schema.CostComponent{certCostComponent},
		UsageSchema:    AcmCertificateUsageSchema,
	}
}

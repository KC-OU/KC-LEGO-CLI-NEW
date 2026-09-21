// Package sync ports sync_service.py's PartDB -> ModernWMS mapping engine:
// categories -> category, parts -> spu+sku (1:1 id), part_lots -> stock
// (seeded only for parts with no existing stock row, so manual ModernWMS-
// side adjustments are never overwritten), plus the fixed bootstrap rows
// (warehouse/location/owner/supplier, the viewonly seed account) the
// original always re-applies idempotently.
package sync

import (
	"strconv"
	"strings"
)

// DeriveSPUCode mirrors the original's spu_code fallback chain: mfg PN,
// else IPN, else the part name, else a synthetic "PART-<id>" placeholder.
func DeriveSPUCode(mfgPN, ipn, name string, partID int) string {
	mfgPN = strings.TrimSpace(mfgPN)
	if mfgPN != "" {
		return mfgPN
	}
	ipn = strings.TrimSpace(ipn)
	if ipn != "" {
		return ipn
	}
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return partPlaceholder(partID)
}

func partPlaceholder(partID int) string {
	return "PART-" + strconv.Itoa(partID)
}

// DeriveSPUName mirrors the original: the part's spec_code (name) if
// present, else its description as a fallback.
func DeriveSPUName(specCode, description string) string {
	specCode = strings.TrimSpace(specCode)
	if specCode != "" {
		return specCode
	}
	return strings.TrimSpace(description)
}

// DeriveSPUDescription mirrors the original's combined description, which
// avoids repeating identical spec_code/description text.
func DeriveSPUDescription(specCode, description string) string {
	specCode = strings.TrimSpace(specCode)
	description = strings.TrimSpace(description)
	if specCode == "" || specCode == description {
		if description != "" {
			return description
		}
		return specCode
	}
	return "Specification Code: " + specCode + " | " + description
}

// DeriveGTIN mirrors the original: gtin if present, else mfg PN, else the
// already-resolved spu_code (so a barcode identifier is always non-empty).
func DeriveGTIN(gtin, mfgPN, spuCode string) string {
	gtin = strings.TrimSpace(gtin)
	if gtin != "" {
		return gtin
	}
	mfgPN = strings.TrimSpace(mfgPN)
	if mfgPN != "" {
		return mfgPN
	}
	return spuCode
}

// DeriveDescriptionSource mirrors the original's part_desc = description or
// comment or spec_code fallback used before SPU description is built.
func DeriveDescriptionSource(description, comment, specCode string) string {
	description = strings.TrimSpace(description)
	if description != "" {
		return description
	}
	comment = strings.TrimSpace(comment)
	if comment != "" {
		return comment
	}
	return specCode
}

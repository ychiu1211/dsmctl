package packagecenter

import "testing"

func TestDecodeCatalogMergesStableAndBeta(t *testing.T) {
	// Shape confirmed live on DSM 7.3.2: stable under "packages", beta under
	// "beta_packages"; per-package id/dname/version/beta/link/md5/size/qinst.
	raw := []byte(`{
		"banners": [],
		"packages": [
			{"id":"SynologyPhotos","dname":"Synology Photos","version":"1.9.1-10928",
			 "link":"https://x/SynologyPhotos.spk","md5":"abc","size":157102285,"qinst":false,"beta":false}
		],
		"beta_packages": [
			{"id":"MailClient","dname":"Synology MailPlus","version":"4.1.0-22322",
			 "link":"https://x/MailPlus.spk","md5":"def","size":63833420,"qinst":true,"beta":true}
		]
	}`)
	catalog, err := decodeCatalog(raw)
	if err != nil {
		t.Fatalf("decodeCatalog() error = %v", err)
	}
	if len(catalog.Packages) != 2 {
		t.Fatalf("decodeCatalog() returned %d packages, want 2", len(catalog.Packages))
	}
	photos := catalog.Packages[0]
	if photos.ID != "SynologyPhotos" || photos.Name != "Synology Photos" || photos.Version != "1.9.1-10928" ||
		photos.DownloadLink != "https://x/SynologyPhotos.spk" || photos.Checksum != "abc" || photos.Size != 157102285 ||
		photos.QuickInstall || photos.Beta {
		t.Fatalf("stable package decode = %#v", photos)
	}
	mail := catalog.Packages[1]
	if mail.ID != "MailClient" || !mail.Beta || !mail.QuickInstall {
		t.Fatalf("beta package decode = %#v", mail)
	}
}

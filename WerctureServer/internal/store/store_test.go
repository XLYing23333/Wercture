package store

import (
	"fmt"
	"testing"
	"time"
)

func TestInsertImagesRollsBackWholeBatch(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateAdmin("admin", "hash"); err != nil {
		t.Fatal(err)
	}
	admin, _ := st.UserByName("admin")
	warehouse, _ := st.CreateWarehouse(admin, "box")
	images := []NewImage{}
	for i := 0; i < 2; i++ {
		images = append(images, NewImage{PublicID: "same-id", Name: fmt.Sprint(i), MIME: "image/png", StoragePath: fmt.Sprintf("files/%d", i), WarehouseID: warehouse.ID, UploaderID: admin.ID})
	}
	if _, err := st.InsertImages(images); err != ErrExists {
		t.Fatalf("expected duplicate failure: %v", err)
	}
	items, _, err := st.ListImages(admin, warehouse.ID, 0, 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial records: %d %v", len(items), err)
	}
}

func TestPurgeExpiredRemovesOldTrash(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateAdmin("admin", "hash-value-ok"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.UserByName("admin")
	if err != nil {
		t.Fatal(err)
	}
	warehouse, err := st.CreateWarehouse(admin, "box")
	if err != nil {
		t.Fatal(err)
	}
	image, err := st.InsertImage(NewImage{
		PublicID: "pub", Name: "a.png", MIME: "image/png", Size: 1, Width: 1, Height: 1,
		StoragePath: "files/missing", WarehouseID: warehouse.ID, UploaderID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TrashImages(admin, []int64{image.ID}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-73 * time.Hour).Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`UPDATE images SET deleted_at=? WHERE id=?`, old, image.ID); err != nil {
		t.Fatal(err)
	}
	n, err := st.PurgeExpired(time.Now())
	if err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	if _, err := st.ImageForUser(admin, image.ID); err != ErrNotFound {
		t.Fatalf("image still present: %v", err)
	}
}

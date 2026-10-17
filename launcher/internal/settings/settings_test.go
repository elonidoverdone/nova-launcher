package settings

import "testing"

func TestOfflineUUID(t *testing.T) {
	// value produced by vanilla for "OfflinePlayer:Notch"
	if got := OfflineUUID("Notch"); got != "b50ad385829d3141a2167e7d7539ba7f" {
		t.Errorf("OfflineUUID(Notch) = %s", got)
	}
}

func TestValidName(t *testing.T) {
	for name, ok := range map[string]bool{"Steve": true, "a_b_9": true, "ab": false, "кирилл": false, "this_is_way_too_long": false, "with space": false} {
		if ValidName(name) != ok {
			t.Errorf("ValidName(%q) != %v", name, ok)
		}
	}
}

func TestStoreAccounts(t *testing.T) {
	dir := t.TempDir()
	st := Open(dir)
	st.Update(func(s *Settings) {
		s.Upsert(Account{ID: "1", Name: "A"})
		s.Upsert(Account{ID: "2", Name: "B"})
		s.Upsert(Account{ID: "1", Name: "A2"})
	})
	s := Open(dir).Get()
	if len(s.Accounts) != 2 || s.Accounts[0].Name != "A2" || s.Selected != "1" {
		t.Fatalf("%+v", s)
	}
	st.Update(func(s *Settings) { s.Remove("1") })
	if got := Open(dir).Get(); got.Selected != "2" || len(got.Accounts) != 1 {
		t.Fatalf("after remove %+v", got)
	}
}

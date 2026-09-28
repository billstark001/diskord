package scope

import "testing"

func TestScope(t *testing.T) {
	for _, h := range []string{"gateway.discord.gg", "gateway-us-east1-b.discord.gg:443"} {
		if !Gateway(h) {
			t.Fatal(h)
		}
	}
	for _, h := range []string{"evilgateway.discord.gg", "gateway.discord.gg.evil.test", "voice.discord.gg", "discord.com.evil.test"} {
		if Gateway(h) || API(h) {
			t.Fatal(h)
		}
	}
	for _, p := range []string{"/api/v10/channels/123/messages", "/api/v9/users/@me", "/api/v9/guilds/123/messages/search", "/api/v9/channels/2/messages/10/reactions/%F0%9F%91%8D"} {
		if Endpoint(p) == "" {
			t.Fatal(p)
		}
	}
	for _, p := range []string{"/api/v9/auth/login", "/api/v9/auth/mfa/totp", "/api/v9/webhooks/123/secret", "/assets/foo", "/api/v9/channels/123/pins"} {
		if Endpoint(p) != "" {
			t.Fatal(p)
		}
	}
	h, p, ok := AssetURL("https://cdn.discordapp.com/attachments/1/2/a.png?ex=secret&hm=secret#secret")
	if !ok || h != "cdn.discordapp.com" || p != "/attachments/1/2/a.png" {
		t.Fatal(h, p, ok)
	}
	if _, _, ok := AssetURL("https://cdn.discordapp.com.evil/a.png"); ok {
		t.Fatal("host escape")
	}
	if ImageExtension("image/svg+xml") != "" {
		t.Fatal("active SVG allowed")
	}
	if AvatarPath("3", "hash") != "/avatars/3/hash.png" || AvatarPath("3", "a_hash") != "/avatars/3/a_hash.gif" || AvatarPath("3", "../evil") != "" {
		t.Fatal("unsafe avatar path")
	}
}

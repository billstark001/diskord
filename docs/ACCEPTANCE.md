# Manual acceptance checklist

Automated tests validate parsing, storage, migration, and the local console. Desktop client, proxy, certificate trust, and the shape of first-party Discord responses must also be checked on each target platform. Use only an account and guild that you are authorized to observe.

1. Follow the platform setup in [PLATFORMS.md](PLATFORMS.md), verify the CA fingerprint, and start diskord in a dedicated runtime directory.
2. Load a guild whose channels have a known nonalphabetical order, at least one uncategorized channel, and one category. Verify that the console puts uncategorized channels first and follows Discord's channel positions within each group.
3. Open a forum channel with several posts. Verify that the console shows post cards, opens each post's thread messages, and shows observed tags, reactions, and cached images. Check both list and gallery controls. Visit an archived post in Discord and verify it appears after the client requests it. Missing unobserved posts are expected.
4. Observe a message in a channel before its guild or parent channel metadata arrives. Verify it does not enter the DM list and that it moves into the guild after the metadata arrives. Verify a real DM still appears in the DM list.
5. Observe a message containing bold, italic, code, a user mention, a channel mention, a custom emoji, and a standard emoji shortcode. Verify the formatting and names appear, while mention-like text inside code remains literal. Check that raw HTML is displayed as text and that Markdown image syntax does not load a remote image.
6. Restart diskord with the same runtime and verify that channel order, forum metadata, and classification survive. Check the dashboard for dropped batches, decoding errors, and lost Gateway synchronization before relying on the archive.
7. Confirm the overview shows the running version and schema version, and message times match the browser's local timezone as `YYYY-MM-DD HH:mm:ss`. Hover a message to copy its Discord URL and raw text; in each all-channel timeline, double-click a message and verify that its source channel or thread opens around it.
8. In a channel with several pages of archived messages, scroll far upward, wait longer than the 10-second refresh interval, then scroll down. Verify newer pages continue to load without reopening the thread.

Do not use a running instance's runtime directory for build or test commands. Stop that instance with `diskord stop-bg` before an acceptance run that reuses its runtime.

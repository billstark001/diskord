package store

// These expressions use the user/attachment aliases shared by archive and
// console queries, so display names and CDN cache variants resolve identically.
const userDisplayNameSQL = `COALESCE(NULLIF(u.display_name,''),NULLIF(u.username,''),u.id,'')`

const attachmentHashSQL = `COALESCE((SELECT r.hash FROM resource_urls r WHERE r.host=a.host AND r.path=a.path),
	(SELECT r.hash FROM resource_urls r WHERE r.host IN ('cdn.discordapp.com','media.discordapp.net') AND r.path=a.path ORDER BY r.host LIMIT 1),'')`

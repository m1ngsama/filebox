# Team

Several people can share one server. An account is an admin, who reaches every volume and manages the others, or a member, who reaches only the volumes granted to them: read, or read and write. A volume nobody granted does not exist for a member: it is missing from lists, search, recent files, photos, storage, duplicates and WebDAV, and its files answer 404. Share links made by a member stop working the moment they lose access to what the link shows. Favorites belong to each person. Tags, the trash and file versions belong to the volume.

The first account is an admin, and so is every account that existed before roles did. Accounts made later start with no access:

```sh
echo 'her-password' | filebox passwd -data /srv/data/filebox -user alice
filebox user -data /srv/data/filebox access alice downloads rw
filebox user -data /srv/data/filebox access alice archive r
filebox user -data /srv/data/filebox ls
filebox user -data /srv/data/filebox role alice admin     # or member
```

A running server notices a change made on the command line within 30 seconds.

Admins manage the team from the Team page instead: invite someone with a one-time link that sets their role and per-volume access (the link works once and expires after 1, 7 or 30 days), change anyone's role or access, make a one-time link for a member who forgot their password (it signs them out everywhere once used), and remove members. There is always at least one admin.

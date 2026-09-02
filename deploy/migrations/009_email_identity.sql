alter table identity.users alter column phone drop not null;
alter table identity.users add column email text;
-- Email identifiers are case-normalized by domain.NewEmail; the unique
-- index enforces the same canonical form so two spellings differing only
-- by case cannot fork two users.
create unique index users_email_unique on identity.users (lower(email));
alter table identity.users add constraint users_identifier_present
  check (phone is not null or email is not null);

alter table identity.login_challenges add column email text;
alter table identity.login_challenges alter column phone drop not null;
alter table identity.login_challenges add constraint login_challenges_one_identifier
  check ((phone is not null) <> (email is not null));

create index login_challenges_email_created_at_idx
  on identity.login_challenges (email, created_at desc);

---- create above / drop below ----

drop index if exists identity.login_challenges_email_created_at_idx;
alter table identity.login_challenges drop constraint if exists login_challenges_one_identifier;
alter table identity.login_challenges drop column if exists email;
alter table identity.login_challenges alter column phone set not null;
alter table identity.users drop constraint if exists users_identifier_present;
drop index if exists identity.users.users_email_unique;
alter table identity.users drop column if exists email;
alter table identity.users alter column phone set not null;

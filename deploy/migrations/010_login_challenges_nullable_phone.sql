alter table identity.login_challenges alter column phone drop not null;

---- create above / drop below ----

alter table identity.login_challenges alter column phone set not null;

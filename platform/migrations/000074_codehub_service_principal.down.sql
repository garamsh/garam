-- Removes codehub's principals and no other: cowork's stay, as 000073
-- wrote them.
DELETE FROM service_principals WHERE grn LIKE 'grn:%/iam/service/codehub';

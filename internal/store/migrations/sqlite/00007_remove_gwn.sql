-- +goose Up
-- Grandstream GWN support was removed: former gwn devices become plain links
-- (they keep opening directly), and the gwn data source and grandstream
-- driver settings are reset.
UPDATE devices SET kind = 'link' WHERE kind = 'gwn';
UPDATE devices SET monitor_source = 'none' WHERE monitor_source = 'gwn';
UPDATE devices SET monitor_driver = 'auto' WHERE monitor_driver = 'grandstream';

-- +goose Down
-- The conversion cannot be undone.
SELECT 1;

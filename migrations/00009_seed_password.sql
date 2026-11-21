-- +goose Up
-- Пароль затравочных пользователей из 00003 (people.v2.csv):
-- 9se3gwWE3Y9j4VU2. Ровно этот хеш стоит дефолтом колонки password_hash
-- в 00001. Миграция перекладывает уже наполненных затравочных пользователей
-- со старого общего хеша на новый: старые тома получают тот же пароль,
-- что и свежие. Пользователи, зарегистрированные через API, не затрагиваются:
-- у каждого свой bcrypt-хеш, совпадений со старым общим хешем у них нет.

UPDATE users
SET password_hash = '$2a$10$W0HrXoeOxu7ocolAw6hvqOD/0Xy/3ztFrSmQx8AEUCEWdtqGctP0C'
WHERE password_hash = '$2a$10$wPU0QusFYid9TqTV5jK21uwTOFHOc5iEvdUz7BGmZh6/Txl9aXsaq';

-- +goose Down
UPDATE users
SET password_hash = '$2a$10$wPU0QusFYid9TqTV5jK21uwTOFHOc5iEvdUz7BGmZh6/Txl9aXsaq'
WHERE password_hash = '$2a$10$W0HrXoeOxu7ocolAw6hvqOD/0Xy/3ztFrSmQx8AEUCEWdtqGctP0C';

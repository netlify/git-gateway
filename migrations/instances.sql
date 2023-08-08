CREATE TABLE IF NOT EXISTS `intances` (
  `id`               VARCHAR    NOT NULL   PRIMARY KEY,
  `uuid`             VARCHAR    NULL,
  `raw_base_config`  TEXT       NULL,
  `base_config`      TEXT       NULL,
  `created_at`       DATETIME   NOT NULL   DEFAULT CURRENT_TIMESTAMP,
  `updated_at`       DATETIME   NULL,
  `deleted_at`       DATETIME   NULL
);

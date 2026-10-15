-- SOJ 初始化脚本（MySQL 容器首次创建数据卷时自动执行）

CREATE TABLE IF NOT EXISTS users (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username      VARCHAR(64)  NOT NULL,
  password_hash VARCHAR(100) NOT NULL,
  nickname      VARCHAR(64)  NOT NULL DEFAULT '',
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_username (username)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS problems (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  title           VARCHAR(128) NOT NULL,
  description     MEDIUMTEXT   NOT NULL,
  time_limit_ms   INT          NOT NULL DEFAULT 1000,
  memory_limit_kb INT          NOT NULL DEFAULT 262144,
  created_at      TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS test_cases (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  problem_id BIGINT UNSIGNED NOT NULL,
  input      MEDIUMTEXT NOT NULL,
  output     MEDIUMTEXT NOT NULL,
  is_sample  TINYINT    NOT NULL DEFAULT 0,
  KEY idx_problem (problem_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS submissions (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id     BIGINT UNSIGNED NOT NULL,
  problem_id  BIGINT UNSIGNED NOT NULL,
  language    VARCHAR(16) NOT NULL,
  code        MEDIUMTEXT NOT NULL,
  status      VARCHAR(32) NOT NULL DEFAULT 'Pending',
  time_ms     INT NOT NULL DEFAULT 0,
  memory_kb   INT NOT NULL DEFAULT 0,
  score       INT NOT NULL DEFAULT 0,
  error_info  TEXT,
  created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  judged_at   TIMESTAMP NULL DEFAULT NULL,
  KEY idx_user (user_id),
  KEY idx_problem (problem_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- ---------------- 种子数据 ----------------

-- 默认管理员：admin / admin123（登录后请尽快修改）
INSERT INTO users (username, password_hash, nickname, role)
VALUES ('admin', '$2a$10$KA5yPtcOFWaUK1AF7PpdzO1JvW5xematz7XnO7CSEQr6EZgSP4QNe', '管理员', 'admin');

INSERT INTO problems (id, title, description, time_limit_ms, memory_limit_kb) VALUES
(1, 'A + B Problem',
'【题目描述】

输入两个整数 a 和 b，输出它们的和。

【输入格式】

一行，两个整数 a 和 b，以空格分隔（-10^9 <= a, b <= 10^9）。

【输出格式】

一行，一个整数，表示 a + b。', 1000, 262144),
(2, 'Hello, SOJ!',
'【题目描述】

开始刷题之前，先和 SOJ 问声好吧！

输出一行信息：Hello, SOJ!

【注意】逗号后有一个空格，行末不要输出多余空格。

【输入格式】

本题没有输入。', 1000, 262144);

INSERT INTO test_cases (problem_id, input, output, is_sample) VALUES
(1, '1 2',     '3',           1),
(1, '100 200', '300',         0),
(1, '-5 7',    '2',           0),
(2, '',        'Hello, SOJ!', 1);

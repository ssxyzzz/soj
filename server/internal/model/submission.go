package model

import (
	"context"
	"fmt"
	"strings"
)

func CreateSubmission(ctx context.Context, userID, problemID int64, language, code string) (int64, error) {
	res, err := db.ExecContext(ctx,
		"INSERT INTO submissions (user_id, problem_id, language, code, status) VALUES (?, ?, ?, ?, ?)",
		userID, problemID, language, code, StatusPending)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const submissionColumns = `
	s.id, s.user_id, u.username, s.problem_id, p.title, s.language,
	s.status, s.time_ms, s.memory_kb, s.score, COALESCE(s.error_info, ''), s.created_at`

const submissionJoins = `
	FROM submissions s
	JOIN users u ON u.id = s.user_id
	JOIN problems p ON p.id = s.problem_id`

func scanSubmission(row interface{ Scan(...any) error }) (*Submission, error) {
	s := &Submission{}
	err := row.Scan(&s.ID, &s.UserID, &s.Username, &s.ProblemID, &s.ProblemTitle, &s.Language,
		&s.Status, &s.TimeMS, &s.MemoryKB, &s.Score, &s.ErrorInfo, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// GetSubmission 返回提交详情（不含代码），代码单独读取以便做可见性控制。
func GetSubmission(ctx context.Context, id int64) (*Submission, error) {
	row := db.QueryRowContext(ctx,
		"SELECT "+submissionColumns+submissionJoins+" WHERE s.id = ?", id)
	return scanSubmission(row)
}

func GetSubmissionCode(ctx context.Context, id int64) (string, error) {
	var code string
	err := db.QueryRowContext(ctx, "SELECT code FROM submissions WHERE id = ?", id).Scan(&code)
	return code, err
}

func UpdateSubmissionStatus(ctx context.Context, id int64, status string) error {
	_, err := db.ExecContext(ctx, "UPDATE submissions SET status = ? WHERE id = ?", status, id)
	return err
}

func FinishSubmission(ctx context.Context, id int64, status string, timeMS, memoryKB, score int, errorInfo string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE submissions
		SET status = ?, time_ms = ?, memory_kb = ?, score = ?, error_info = ?, judged_at = NOW()
		WHERE id = ?`, status, timeMS, memoryKB, score, errorInfo, id)
	return err
}

// ListSubmissions 按可选条件（user_id / problem_id，0 表示不过滤）分页查询。
func ListSubmissions(ctx context.Context, userID, problemID int64, page, pageSize int) ([]*Submission, int64, error) {
	where := []string{"1=1"}
	args := []any{}
	if userID > 0 {
		where = append(where, "s.user_id = ?")
		args = append(args, userID)
	}
	if problemID > 0 {
		where = append(where, "s.problem_id = ?")
		args = append(args, problemID)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM submissions s JOIN users u ON u.id = s.user_id JOIN problems p ON p.id = s.problem_id WHERE "+whereSQL,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf("SELECT %s %s WHERE %s ORDER BY s.id DESC LIMIT ? OFFSET ?",
		submissionColumns, submissionJoins, whereSQL)
	rows, err := db.QueryContext(ctx, query, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*Submission, 0)
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, s)
	}
	return list, total, rows.Err()
}

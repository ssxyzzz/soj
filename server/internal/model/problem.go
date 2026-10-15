package model

import "context"

// ListProblems 返回题库分页列表与总数。
func ListProblems(ctx context.Context, page, pageSize int) ([]*Problem, int64, error) {
	var total int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM problems").Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := db.QueryContext(ctx,
		"SELECT id, title, time_limit_ms, memory_limit_kb FROM problems ORDER BY id LIMIT ? OFFSET ?",
		pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*Problem, 0)
	for rows.Next() {
		p := &Problem{}
		if err := rows.Scan(&p.ID, &p.Title, &p.TimeLimitMS, &p.MemoryLimitKB); err != nil {
			return nil, 0, err
		}
		list = append(list, p)
	}
	return list, total, rows.Err()
}

func GetProblem(ctx context.Context, id int64) (*Problem, error) {
	row := db.QueryRowContext(ctx,
		"SELECT id, title, description, time_limit_ms, memory_limit_kb, created_at FROM problems WHERE id = ?", id)
	p := &Problem{}
	if err := row.Scan(&p.ID, &p.Title, &p.Description, &p.TimeLimitMS, &p.MemoryLimitKB, &p.CreatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListSampleCases 返回题目的样例（前端展示用）。
func ListSampleCases(ctx context.Context, problemID int64) ([]*TestCase, error) {
	return listCases(ctx, problemID, true)
}

// ListAllCases 返回题目全部测试点（判题用）。
func ListAllCases(ctx context.Context, problemID int64) ([]*TestCase, error) {
	return listCases(ctx, problemID, false)
}

func listCases(ctx context.Context, problemID int64, sampleOnly bool) ([]*TestCase, error) {
	query := "SELECT id, problem_id, input, output, is_sample FROM test_cases WHERE problem_id = ?"
	if sampleOnly {
		query += " AND is_sample = 1"
	}
	query += " ORDER BY id"

	rows, err := db.QueryContext(ctx, query, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*TestCase, 0)
	for rows.Next() {
		tc := &TestCase{}
		if err := rows.Scan(&tc.ID, &tc.ProblemID, &tc.Input, &tc.Output, &tc.IsSample); err != nil {
			return nil, err
		}
		list = append(list, tc)
	}
	return list, rows.Err()
}

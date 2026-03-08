#!/usr/bin/env bash
# Sprint — the direct-execution unit.
# You prompt. It runs. No plan, no approval, no confirmation.

sprint_create() {
    local goal="$1" repo_url="$2" repo_path="$3" branch="$4" test_cmd="$5" lint_cmd="$6"

    if [ -z "$goal" ]; then
        echo "Usage: telecoder sprint <goal> [options]" >&2
        return 1
    fi

    # Create a session for this sprint
    local session_id
    session_id=$(session_create "$repo_url" "$repo_path" "$branch" "$test_cmd" "$lint_cmd")

    # Create the sprint record
    local sprint_id
    sprint_id=$(head -c4 /dev/urandom | xxd -p)

    db_param_exec \
        "INSERT INTO sprints (id, session_id, goal, status) VALUES (:id, :session_id, :goal, 'running')" \
        id "$sprint_id" \
        session_id "$session_id" \
        goal "$goal"

    # Execute immediately — no planning, no confirmation
    session_run "$session_id" "$goal"

    echo "sprint:${sprint_id} session:${session_id}"
}

sprint_create_in_session() {
    local session_id="$1" goal="$2"

    if [ -z "$session_id" ] || [ -z "$goal" ]; then
        echo "Usage: sprint_create_in_session <session-id> <goal>" >&2
        return 1
    fi

    # Verify session exists
    local workspace
    workspace=$(db_query "SELECT workspace FROM sessions WHERE id='${session_id}';")
    if [ -z "$workspace" ]; then
        echo "Session not found: $session_id" >&2
        return 1
    fi

    local sprint_id
    sprint_id=$(head -c4 /dev/urandom | xxd -p)

    db_param_exec \
        "INSERT INTO sprints (id, session_id, goal, status) VALUES (:id, :session_id, :goal, 'running')" \
        id "$sprint_id" \
        session_id "$session_id" \
        goal "$goal"

    # Execute immediately
    session_run "$session_id" "$goal"

    echo "sprint:${sprint_id} session:${session_id}"
}

sprint_status() {
    local sprint_id="$1"
    local row
    row=$(db_query "SELECT s.id, s.session_id, s.goal, s.status, s.result, s.created_at, s.completed_at, se.status FROM sprints s JOIN sessions se ON s.session_id = se.id WHERE s.id='${sprint_id}';")

    if [ -z "$row" ]; then
        echo "Sprint not found: $sprint_id" >&2
        return 1
    fi

    IFS='|' read -r sid ssession sgoal sstatus sresult screated scompleted ssession_status <<< "$row"

    # Auto-update sprint status based on session
    local tmux_name="tc-${ssession}"
    if [ "$sstatus" = "running" ] && ! tmux has-session -t "$tmux_name" 2>/dev/null; then
        # Session finished — check verification
        local verify_result="skipped"
        local test_cmd lint_cmd
        IFS='|' read -r test_cmd lint_cmd <<< "$(db_query "SELECT test_cmd, lint_cmd FROM sessions WHERE id='${ssession}';")"

        if [ -n "$test_cmd" ] || [ -n "$lint_cmd" ]; then
            if session_verify "$ssession" >/dev/null 2>&1; then
                verify_result="passed"
            else
                verify_result="failed"
            fi
        fi

        local new_status="completed"
        local result_text="done (verify: ${verify_result})"

        # Auto-push if configured
        if [ "$TELECODER_AUTO_PUSH" = "true" ]; then
            local workspace
            workspace=$(db_query "SELECT workspace FROM sessions WHERE id='${ssession}';")
            if [ -d "${workspace}/.git" ]; then
                if git -C "$workspace" push origin HEAD 2>/dev/null; then
                    result_text="done (verify: ${verify_result}, pushed)"
                else
                    result_text="done (verify: ${verify_result}, push failed)"
                fi
            fi
        fi

        db_param_exec \
            "UPDATE sprints SET status=:status, result=:result, completed_at=datetime('now') WHERE id=:id" \
            status "$new_status" \
            result "$result_text" \
            id "$sprint_id"

        sstatus="$new_status"
        sresult="$result_text"
    fi

    echo "Sprint:     $sid"
    echo "Session:    $ssession"
    echo "Status:     $sstatus"
    echo "Goal:       $sgoal"
    if [ -n "$sresult" ]; then
        echo "Result:     $sresult"
    fi
    echo "Created:    $screated"
    if [ -n "$scompleted" ]; then
        echo "Completed:  $scompleted"
    fi
}

sprint_list() {
    local status_filter="$1"
    local query="SELECT s.id, s.session_id, s.status, s.goal, s.created_at FROM sprints s"
    if [ -n "$status_filter" ]; then
        query="${query} WHERE s.status='${status_filter}'"
    fi
    query="${query} ORDER BY s.created_at DESC;"

    printf "%-10s %-14s %-12s %-30s %s\n" "SPRINT" "SESSION" "STATUS" "GOAL" "CREATED"
    printf '%s\n' "$(printf '%.0s-' {1..90})"

    db_query "$query" | while IFS='|' read -r sid ssession sstatus sgoal screated; do
        if [ ${#sgoal} -gt 28 ]; then
            sgoal="${sgoal:0:25}..."
        fi
        printf "%-10s %-14s %-12s %-30s %s\n" "$sid" "$ssession" "$sstatus" "$sgoal" "$screated"
    done
}

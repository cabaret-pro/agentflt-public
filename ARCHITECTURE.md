```mermaid
flowchart TB
    subgraph CLI["CLI Commands"]
        new["new - Create agent"]
        list["list - List sessions"]
        dashboard["dashboard - TUI"]
        attach["attach - Tmux attach"]
        restart["restart - Restart agent"]
        stop["stop - Stop agent"]
        logs["logs - View output"]
    end

    subgraph TUI["TUI Dashboard"]
        dashboard_go["dashboard.go"]
        views["Views"]
        fleet["Fleet View"]
        grid["Grid View"]
        focus["Focus View"]
        timeline["Timeline"]
        editor["Embedded Editor"]
    end

    subgraph Core["Core Packages"]
        supervisor["supervisor.go"]
        tmux["tmux.go"]
        store["store.go"]
        git["git.go"]
        filetree["filetree.go"]
    end

    subgraph Data["Data Layer"]
        db[("SQLite DB")]
        sessions["Sessions Table"]
        events["Events Table"]
    end

    subgraph External["External Systems"]
        tmux_ext["tmux"]
        git_ext["git"]
        terminal["Terminal"]
    end

    CLI --> dashboard_go
    dashboard_go --> views
    views --> fleet
    views --> grid
    views --> focus
    views --> timeline
    focus --> editor

    dashboard_go --> supervisor
    supervisor --> tmux
    supervisor --> store
    supervisor --> git
    
    tmux --> store
    store --> db
    git --> filetree
    
    tmux --> tmux_ext
    git --> git_ext
    editor --> terminal

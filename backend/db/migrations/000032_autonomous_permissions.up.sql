INSERT INTO permissions (key, display_name) VALUES
    ('autonomous.start', 'Start autonomous workflow'),
    ('autonomous.retry', 'Retry autonomous workflow'),
    ('autonomous.cancel', 'Cancel autonomous workflow'),
    ('ai_policy.manage', 'Manage AI execution policy'),
    ('environment.manage', 'Manage project environments'),
    ('deployment.approve', 'Approve deployment')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT role_key, permission_key
FROM (VALUES
    ('owner', 'autonomous.start'), ('owner', 'autonomous.retry'), ('owner', 'autonomous.cancel'), ('owner', 'ai_policy.manage'), ('owner', 'environment.manage'), ('owner', 'deployment.approve'),
    ('admin', 'autonomous.start'), ('admin', 'autonomous.retry'), ('admin', 'autonomous.cancel'), ('admin', 'ai_policy.manage'), ('admin', 'environment.manage'), ('admin', 'deployment.approve'),
    ('project_manager', 'autonomous.start'), ('project_manager', 'autonomous.retry'), ('project_manager', 'autonomous.cancel'), ('project_manager', 'ai_policy.manage'), ('project_manager', 'environment.manage'), ('project_manager', 'deployment.approve'),
    ('developer', 'autonomous.retry'), ('developer', 'autonomous.cancel'),
    ('qa', 'autonomous.retry'), ('qa', 'deployment.approve')
) AS permissions(role_key, permission_key)
ON CONFLICT DO NOTHING;

DELETE FROM role_permissions WHERE permission_key IN ('autonomous.start','autonomous.retry','autonomous.cancel','ai_policy.manage','environment.manage','deployment.approve');
DELETE FROM permissions WHERE key IN ('autonomous.start','autonomous.retry','autonomous.cancel','ai_policy.manage','environment.manage','deployment.approve');

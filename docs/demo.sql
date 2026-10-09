-- Synthetic Things library for docs/demo.tape.
-- Load it after internal/testutil/testdata/v24.sql.
-- Dates are relative to the day of recording.

CREATE TEMP VIEW day AS SELECT
    (CAST(strftime('%Y', 'now', 'localtime') AS INTEGER) << 16)
  | (CAST(strftime('%m', 'now', 'localtime') AS INTEGER) << 12)
  | (CAST(strftime('%d', 'now', 'localtime') AS INTEGER) << 7) AS today,
    (CAST(strftime('%Y', 'now', 'localtime', '-1 day') AS INTEGER) << 16)
  | (CAST(strftime('%m', 'now', 'localtime', '-1 day') AS INTEGER) << 12)
  | (CAST(strftime('%d', 'now', 'localtime', '-1 day') AS INTEGER) << 7) AS yesterday,
    (CAST(strftime('%Y', 'now', 'localtime', '+5 days') AS INTEGER) << 16)
  | (CAST(strftime('%m', 'now', 'localtime', '+5 days') AS INTEGER) << 12)
  | (CAST(strftime('%d', 'now', 'localtime', '+5 days') AS INTEGER) << 7) AS later;

INSERT INTO TMArea (uuid, title, "index") VALUES
    ('8sJk2LmQw4XcVn7RtYp3Bd', 'Work', 0),
    ('Fq9WzN2hTj6Kc4LmXs8VbR', 'Personal', 1);

INSERT INTO TMTag (uuid, title, "index") VALUES
    ('Tg4Hs8Kd2Lq6Mw9Px3Rv7B', 'writing', 0),
    ('Er5Nd7Qa3Ws9Zc1Xv6Bn2M', 'errand', 1);

-- Project and headings.
INSERT INTO TMTask (uuid, type, title, notes, start, area, "index") VALUES
    ('Wr3Fh8Kp2Lx5Nq9Tz4Vc7J', 1, 'Website refresh', 'Ship the new landing page before the October launch.', 1, '8sJk2LmQw4XcVn7RtYp3Bd', 0);
INSERT INTO TMTask (uuid, type, title, start, project, "index") VALUES
    ('Hd6Gn2Mk8Rp4Ls1Qw9Xt3Z', 2, 'Design', 1, 'Wr3Fh8Kp2Lx5Nq9Tz4Vc7J', 0),
    ('Hl9Ch4Nv2Bx7Kq5Ms3Jr8W', 2, 'Launch', 1, 'Wr3Fh8Kp2Lx5Nq9Tz4Vc7J', 1);

-- Project tasks.
INSERT INTO TMTask (uuid, title, notes, start, startDate, todayIndex, heading, "index")
    SELECT 'Rv7Lp2Kq9Xm4Nw6Bz3Tc8H', 'Review landing page copy', 'Check tone and length.', 1, today, 1, 'Hd6Gn2Mk8Rp4Ls1Qw9Xt3Z', 0 FROM day;
INSERT INTO TMTask (uuid, title, start, heading, "index") VALUES
    ('Ch5Hm9Qx2Lr7Kp4Wn8Zv3D', 'Choose hero image', 1, 'Hd6Gn2Mk8Rp4Ls1Qw9Xt3Z', 1),
    ('Wl3An7Kx9Qp2Mr6Zc4Vn8L', 'Write launch announcement', 1, 'Hl9Ch4Nv2Bx7Kq5Ms3Jr8W', 0),
    ('Su6Rd2Lk8Xw4Nq9Pm3Bz7C', 'Set up redirects', 1, 'Hl9Ch4Nv2Bx7Kq5Ms3Jr8W', 1);

-- Today, including an evening task and an overdue deadline.
INSERT INTO TMTask (uuid, title, start, startDate, startBucket, todayIndex, reminderTime, area)
    SELECT 'Pk4Dc8Wn2Qr6Zl9Mx3Vb7S', 'Pick up dry cleaning', 1, today, 0, 2, NULL, 'Fq9WzN2hTj6Kc4LmXs8VbR' FROM day
    UNION ALL
    SELECT 'Hk2Wp7Ln5Qz9Xc4Rm8Bv3T', 'Plan weekend hike', 1, today, 1, 3, (18 << 26) | (30 << 20), 'Fq9WzN2hTj6Kc4LmXs8VbR' FROM day;
INSERT INTO TMTask (uuid, title, start, deadline, area)
    SELECT 'Sx8Er3Kp6Wn2Lq9Zm4Vc7F', 'Submit expense report', 1, yesterday, '8sJk2LmQw4XcVn7RtYp3Bd' FROM day;

-- Inbox, Upcoming, Someday, and Logbook.
INSERT INTO TMTask (uuid, title, start) VALUES
    ('Cd4Dn8Wq2Lx6Kp9Zr3Mv7N', 'Call the dentist', 0),
    ('Ls2Sw6Kq9Xp3Nm7Zr4Vc8K', 'Learn Swedish', 2);
INSERT INTO TMTask (uuid, title, start, startDate)
    SELECT 'Rp9Ps3Kw7Lx2Nq5Zm8Vc4G', 'Renew passport', 2, later FROM day;
INSERT INTO TMTask (uuid, title, status, start, stopDate, area) VALUES
    ('Bf7Fl3Kp9Wx2Nq6Zm4Rc8P', 'Book flights', 3, 1, strftime('%s', 'now', '-1 day'), 'Fq9WzN2hTj6Kc4LmXs8VbR');

INSERT INTO TMTaskTag (tasks, tags) VALUES
    ('Rv7Lp2Kq9Xm4Nw6Bz3Tc8H', 'Tg4Hs8Kd2Lq6Mw9Px3Rv7B'),
    ('Wl3An7Kx9Qp2Mr6Zc4Vn8L', 'Tg4Hs8Kd2Lq6Mw9Px3Rv7B'),
    ('Pk4Dc8Wn2Qr6Zl9Mx3Vb7S', 'Er5Nd7Qa3Ws9Zc1Xv6Bn2M');

INSERT INTO TMChecklistItem (uuid, title, status, task, "index") VALUES
    ('Ck1Dr8Fa3Lp6Wq2Zx9Mn4V', 'Draft the post', 3, 'Wl3An7Kx9Qp2Mr6Zc4Vn8L', 0),
    ('Ck2Fb5Gt7Kp3Wn9Zr4Mx8L', 'Get feedback', 0, 'Wl3An7Kx9Qp2Mr6Zc4Vn8L', 1),
    ('Ck3Sp9Hd2Lq6Wx4Zn8Mv7R', 'Schedule the announcement', 0, 'Wl3An7Kx9Qp2Mr6Zc4Vn8L', 2);

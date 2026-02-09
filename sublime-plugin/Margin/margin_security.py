import os


def _is_within_root(root, candidate):
    try:
        rel = os.path.relpath(candidate, root)
    except ValueError:
        return False
    if rel == os.curdir:
        return True
    return rel != os.pardir and not rel.startswith(os.pardir + os.sep)


def resolve_within_root(root, path_value):
    candidate = os.path.join(root, str(path_value or ""))
    resolved_root = os.path.realpath(root)
    resolved_candidate = os.path.realpath(candidate)
    if not _is_within_root(resolved_root, resolved_candidate):
        raise ValueError("Invalid path returned by CLI: {}".format(path_value))
    return resolved_candidate

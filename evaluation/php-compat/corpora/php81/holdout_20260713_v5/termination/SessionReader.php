<?php

declare(strict_types=1);

namespace RetrievalHoldout\Termination;

require_once __DIR__ . '/end_session.php';

final class SessionReader
{
    public function read(string $key): ?string
    {
        if ($key === '') {
            endSession('A session key is required.');
        }

        return null;
    }
}

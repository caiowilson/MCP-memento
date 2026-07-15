<?php

declare(strict_types=1);

namespace App\Service;

use App\Repository\AuditLogRepository;

final class AuditReader
{
    public function __construct(private AuditLogRepository $repository)
    {
    }
}

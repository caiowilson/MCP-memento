<?php

declare(strict_types=1);

namespace Fixture\PHP80\Handler;

use Fixture\PHP80\Attribute\AuditTag;

final class AuditedHandler
{
    #[AuditTag('security')]
    public function handle(): void
    {
    }
}

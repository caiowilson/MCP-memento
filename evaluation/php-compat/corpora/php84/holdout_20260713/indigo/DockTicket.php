<?php
declare(strict_types=1);

namespace Holdout\Php84\Indigo;

final class DockTicket
{
    public string $code {
        set => strtoupper(trim($value));
    }
}

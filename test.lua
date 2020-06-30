local cl = require "libclua"
cl.start("test.cov", 5)

function test1(i) then
    if i % 2 then
        print(i)
    else
        print(i)
    end
end

function test2(i) then
    if i > 30 then
        print(i)
    else
        print(i)
    end
end

function test3(i) then

    if i > 0 then
        print(i)
    else
        print(i)
    end

end

for i = 0, 100 then
    if i < 10 then
        test1(i)
    elseif i < 50 then
        test2(i)
    else
        test3(i)
    end
end

cl.stop()
